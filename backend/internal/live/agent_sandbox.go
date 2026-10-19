package live

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	sandboxCodeLimit      = 48 * 1024
	sandboxFileLimit      = 12
	sandboxInputLimit     = 256 * 1024
	sandboxOutputLimit    = 16 * 1024
	sandboxWorkspaceLimit = 2 * 1024 * 1024
	sandboxEntryLimit     = 64
)

// Bound aggregate memory use even when an authenticated user makes concurrent
// requests across several conversations.
var agentSandboxSlots = make(chan struct{}, 2)

type sandboxInputFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type sandboxResult struct {
	Stdout   string            `json:"stdout"`
	Stderr   string            `json:"stderr,omitempty"`
	ExitCode int               `json:"exitCode"`
	Files    map[string]string `json:"files,omitempty"`
	CodeHash string            `json:"-"`
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func agentSandboxAvailable() bool {
	return agentSandboxRuntimeAvailable("/usr/bin/python3")
}

func agentShellAvailable() bool {
	return agentSandboxRuntimeAvailable("/usr/bin/bash")
}

func agentSandboxRuntimeAvailable(runtime string) bool {
	if os.Geteuid() == 0 {
		return false
	}
	sharedMemory, err := os.Stat("/dev/shm")
	if err != nil || !sharedMemory.IsDir() {
		return false
	}
	for _, binary := range []string{"/usr/bin/bwrap", "/usr/bin/prlimit", runtime} {
		if _, err := os.Stat(binary); err != nil {
			return false
		}
	}
	return true
}

func runPythonSandbox(ctx context.Context, code string, files []sandboxInputFile) (sandboxResult, error) {
	return runSandboxProgram(ctx, code, files, "main.py", []string{"/usr/bin/python3", "-I", "-S"})
}

func runShellSandbox(ctx context.Context, script string, files []sandboxInputFile) (sandboxResult, error) {
	return runSandboxProgram(ctx, script, files, "main.sh", []string{"/usr/bin/bash", "--noprofile", "--norc", "--noediting"})
}

func runSandboxProgram(ctx context.Context, program string, files []sandboxInputFile, entrypoint string, runtime []string) (sandboxResult, error) {
	var result sandboxResult
	if err := validateSandboxProgram(entrypoint, program, files); err != nil {
		return result, err
	}
	if len(runtime) == 0 || !agentSandboxRuntimeAvailable(runtime[0]) {
		return result, errors.New("隔离沙箱不可用：需要以非 root 用户运行并安装 bubblewrap、prlimit 和所选运行时")
	}

	// Keep attacker-controlled output on ephemeral memory storage. The mounted
	// workspace is monitored for both total bytes and inode count below.
	workspace, err := os.MkdirTemp("/dev/shm", "teammind-agent-sandbox-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(workspace)
	if err = os.Chmod(workspace, 0700); err != nil {
		return result, err
	}
	writeSandboxFile := func(name string, content []byte) error {
		destination := filepath.Join(workspace, name)
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		return os.WriteFile(destination, content, 0600)
	}
	if err = writeSandboxFile(entrypoint, []byte(program)); err != nil {
		return result, err
	}
	for _, file := range files {
		if err = writeSandboxFile(file.Name, []byte(file.Content)); err != nil {
			return result, err
		}
	}
	command := []string{
		"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL",
		"--ro-bind", "/usr", "/usr", "--ro-bind", "/lib", "/lib", "--ro-bind", "/lib64", "/lib64",
		"--dev", "/dev", "--tmpfs", "/dev/shm", "--bind", workspace, "/dev/shm", "--proc", "/proc",
		"--dir", "/tmp", "--bind", workspace, "/tmp", "--bind", workspace, "/work", "--remount-ro", "/",
		"--chdir", "/work", "--clearenv", "--setenv", "PATH", "/usr/bin", "--setenv", "HOME", "/work", "--setenv", "TMPDIR", "/work",
		"--setenv", "LANG", "C.UTF-8", "/usr/bin/prlimit", "--cpu=3:3", "--as=268435456:268435456",
		"--fsize=1048576:1048576", "--nofile=32:32", "--nproc=8:8", "--",
	}
	command = append(command, runtime...)
	command = append(command, "/work/"+entrypoint)
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmdCtx, cancelCommand := context.WithCancel(ctx)
	defer cancelCommand()
	cmd := exec.CommandContext(cmdCtx, "/usr/bin/bwrap", command...)
	cmd.Env = []string{"PATH=/usr/bin", "LANG=C", "HOME=/tmp"}
	stdout := &boundedBuffer{limit: sandboxOutputLimit}
	stderr := &boundedBuffer{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	monitorStop := make(chan struct{})
	monitorDone := make(chan struct{})
	resourceExceeded := make(chan error, 1)
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := checkSandboxWorkspace(workspace); err != nil {
				select {
				case resourceExceeded <- err:
				default:
				}
				cancelCommand()
				return
			}
			select {
			case <-monitorStop:
				return
			case <-ticker.C:
			}
		}
	}()
	runErr := cmd.Run()
	close(monitorStop)
	<-monitorDone
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	digest := sha256.Sum256([]byte(program))
	result.CodeHash = hex.EncodeToString(digest[:])
	result.Files = make(map[string]string)
	if err := checkSandboxWorkspace(workspace); err != nil {
		return result, fmt.Errorf("沙箱临时工作区超过总文件或空间限制")
	}
	select {
	case <-resourceExceeded:
		return result, fmt.Errorf("沙箱临时工作区超过总文件或空间限制")
	default:
	}
	_ = filepath.WalkDir(workspace, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || name == filepath.Join(workspace, entrypoint) {
			return nil
		}
		info, err := os.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(workspace, name)
		if err != nil || len(result.Files) >= 12 {
			return nil
		}
		content, err := os.ReadFile(name)
		if err != nil {
			return nil
		}
		if len(content) > 64*1024 {
			content = content[:64*1024]
		}
		result.Files[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	if runErr != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("沙箱执行超时或超出资源限制")
		}
		select {
		case <-resourceExceeded:
			return result, fmt.Errorf("沙箱临时工作区超过总文件或空间限制")
		default:
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("沙箱启动失败")
		}
	} else {
		result.ExitCode = 0
	}
	return result, nil
}

// Bubblewrap keeps the workspace inside the mount namespace, and this monitor
// limits aggregate storage and inode use while untrusted code is running. A
// per-process RLIMIT_FSIZE alone is insufficient because a child can create
// many individually small files.
func checkSandboxWorkspace(root string) error {
	entries := 0
	var total int64
	pending := []string{root}
	for len(pending) > 0 {
		dirPath := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		dir, err := os.Open(dirPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		for {
			children, readErr := dir.ReadDir(16)
			for _, child := range children {
				entries++
				if entries > sandboxEntryLimit {
					dir.Close()
					return errors.New("sandbox workspace entry limit exceeded")
				}
				path := filepath.Join(dirPath, child.Name())
				if child.IsDir() {
					pending = append(pending, path)
					continue
				}
				info, statErr := child.Info()
				if statErr != nil {
					if errors.Is(statErr, os.ErrNotExist) {
						continue
					}
					dir.Close()
					return statErr
				}
				if info.Mode().IsRegular() {
					total += info.Size()
					if total > sandboxWorkspaceLimit {
						dir.Close()
						return errors.New("sandbox workspace byte limit exceeded")
					}
				}
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				dir.Close()
				return readErr
			}
		}
		if err = dir.Close(); err != nil {
			return err
		}
	}
	return nil
}

func validateSandboxInput(code string, files []sandboxInputFile) error {
	return validateSandboxProgram("main.py", code, files)
}

func validateSandboxProgram(entrypoint, program string, files []sandboxInputFile) error {
	if len(strings.TrimSpace(program)) == 0 || len(program) > sandboxCodeLimit || len(files) > sandboxFileLimit {
		return errors.New("沙箱代码或文件数量超出限制")
	}
	if entrypoint != "main.py" && entrypoint != "main.sh" {
		return errors.New("沙箱入口类型不受支持")
	}
	for _, file := range files {
		if strings.EqualFold(file.Name, entrypoint) {
			return errors.New("输入文件不能覆盖沙箱入口文件")
		}
	}
	return validateSandboxFiles(files)
}

func validateSandboxFiles(files []sandboxInputFile) error {
	if len(files) > sandboxFileLimit {
		return errors.New("沙箱输入文件数量超出限制")
	}
	total := 0
	seenFiles := make(map[string]struct{}, len(files))
	for _, file := range files {
		total += len(file.Content)
		if total > sandboxInputLimit || len(file.Name) == 0 || len(file.Name) > 160 || len(file.Content) > 64*1024 || strings.ContainsAny(file.Name, "\\\r\n\t") {
			return errors.New("沙箱输入文件超出大小限制")
		}
		clean := filepath.Clean(file.Name)
		if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean != file.Name || strings.ContainsRune(file.Name, 0) {
			return errors.New("沙箱文件名只能是工作区内的相对路径")
		}
		if strings.EqualFold(clean, "main.py") || strings.EqualFold(clean, "main.sh") {
			return errors.New("main.py 和 main.sh 是沙箱入口文件，不能作为输入文件覆盖")
		}
		if _, exists := seenFiles[clean]; exists {
			return errors.New("沙箱输入文件名不能重复")
		}
		seenFiles[clean] = struct{}{}
	}
	return nil
}

var _ io.Writer = (*boundedBuffer)(nil)
