package live

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const sandboxSessionTTL = 30 * time.Minute

type sandboxSession struct {
	ID          string
	UserID      string
	Runtime     string
	ContainerID string
	Workspace   string
	Status      string
	ExpiresAt   time.Time
}

func (s *Server) sandboxSessionsRoute(w http.ResponseWriter, r *http.Request, u User, path string) error {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// me/sandbox/sessions
	if len(parts) == 3 && parts[0] == "me" && parts[1] == "sandbox" && parts[2] == "sessions" {
		switch r.Method {
		case http.MethodGet:
			return s.listSandboxSessions(w, r, u)
		case http.MethodPost:
			return s.createSandboxSession(w, r, u)
		default:
			return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
	}
	if len(parts) == 5 && parts[0] == "me" && parts[1] == "sandbox" && parts[2] == "sessions" && parts[4] == "exec" {
		if e := method(r, http.MethodPost); e != nil {
			return e
		}
		if !validID(parts[3]) {
			return bad(400, "INVALID_ID", "沙箱会话 ID 无效")
		}
		return s.execSandboxSession(w, r, u, parts[3])
	}
	if len(parts) == 4 && parts[0] == "me" && parts[1] == "sandbox" && parts[2] == "sessions" {
		if e := method(r, http.MethodDelete); e != nil {
			return e
		}
		if !validID(parts[3]) {
			return bad(400, "INVALID_ID", "沙箱会话 ID 无效")
		}
		return s.stopSandboxSession(w, r, u, parts[3])
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}

func (s *Server) listSandboxSessions(w http.ResponseWriter, r *http.Request, u User) error {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(id),runtime,container_id,status,created_at,last_used_at,expires_at
		FROM sandbox_sessions WHERE user_id=UNHEX(?) AND status IN ('active','stopped') ORDER BY created_at DESC LIMIT 20`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, runtime, container, status string
		var created, used, expires time.Time
		if err = rows.Scan(&id, &runtime, &container, &status, &created, &used, &expires); err != nil {
			return err
		}
		items = append(items, map[string]any{"id": id, "runtime": runtime, "status": status, "createdAt": created, "lastUsedAt": used, "expiresAt": expires})
	}
	write(w, 200, items)
	return rows.Err()
}

func (s *Server) createSandboxSession(w http.ResponseWriter, r *http.Request, u User) error {
	if !dockerSandboxAvailable(sandboxPythonImage) {
		return bad(http.StatusServiceUnavailable, "SANDBOX_RUNTIME_UNAVAILABLE", "沙箱 runtime 需要 Docker 和 teanmind-sandbox-python:local 镜像")
	}
	var active int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sandbox_sessions WHERE user_id=UNHEX(?) AND status='active' AND expires_at>UTC_TIMESTAMP(3)`, u.ID).Scan(&active); err != nil {
		return err
	}
	if active >= 2 {
		return bad(409, "SANDBOX_SESSION_LIMIT", "最多同时保留 2 个沙箱会话")
	}
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return bad(503, "SANDBOX_RUNTIME_UNAVAILABLE", "未找到 docker 命令")
	}
	sessionID := id()
	workspace := filepath.Join(sandboxWorkspaceRoot(), "sessions", sessionID)
	if err = os.MkdirAll(workspace, 0777); err != nil {
		return err
	}
	_ = os.Chmod(workspace, 0777)
	container := "teammind-sbx-" + strings.ToLower(sessionID[:16])
	args := []string{
		"run", "-d", "--rm", "--name", container,
		"--network", "none", "--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,size=1m",
		"--memory", "256m", "--memory-swap", "256m",
		"--pids-limit", "64", "--cpus", "1",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--user", "65534:65534",
		"-v", workspace + ":/work:rw", "-w", "/work",
		sandboxPythonImage, "sleep", "infinity",
	}
	runCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, dockerPath, args...)
	if output, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.RemoveAll(workspace)
		return bad(502, "SANDBOX_RUNTIME_FAILED", "沙箱容器启动失败："+truncateRunes(strings.TrimSpace(string(output)), 200))
	}
	expires := time.Now().UTC().Add(sandboxSessionTTL)
	if _, err = s.DB.ExecContext(r.Context(), `INSERT INTO sandbox_sessions(id,user_id,runtime,container_id,workspace_path,status,expires_at)
		VALUES(UNHEX(?),UNHEX(?),'docker',?,?,'active',?)`, sessionID, u.ID, container, workspace, expires); err != nil {
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = exec.CommandContext(killCtx, dockerPath, "rm", "-f", container).Run()
		killCancel()
		_ = os.RemoveAll(workspace)
		return err
	}
	write(w, 201, map[string]any{"id": sessionID, "runtime": "docker", "status": "active", "expiresAt": expires})
	return nil
}

func (s *Server) sandboxSessionByID(ctx context.Context, userID, sessionID string) (sandboxSession, error) {
	var session sandboxSession
	err := s.DB.QueryRowContext(ctx, `SELECT HEX(id),HEX(user_id),runtime,container_id,workspace_path,status,expires_at
		FROM sandbox_sessions WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, sessionID, userID).Scan(
		&session.ID, &session.UserID, &session.Runtime, &session.ContainerID, &session.Workspace, &session.Status, &session.ExpiresAt)
	return session, err
}

func (s *Server) execSandboxSession(w http.ResponseWriter, r *http.Request, u User, sessionID string) error {
	session, err := s.sandboxSessionByID(r.Context(), u.ID, sessionID)
	if err == sql.ErrNoRows {
		return bad(404, "SANDBOX_SESSION_NOT_FOUND", "沙箱会话不存在")
	}
	if err != nil {
		return err
	}
	if session.Status != "active" {
		return bad(410, "SANDBOX_SESSION_CLOSED", "沙箱会话已关闭")
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.stopSandboxSessionInternal(context.Background(), session)
		return bad(410, "SANDBOX_SESSION_EXPIRED", "沙箱会话已过期")
	}
	var input struct {
		Language string             `json:"language"`
		Code     string             `json:"code"`
		Files    []sandboxInputFile `json:"files"`
	}
	if err = body(w, r, &input); err != nil {
		return err
	}
	if input.Language != "python" && input.Language != "shell" {
		return bad(400, "VALIDATION_ERROR", "language 必须是 python 或 shell")
	}
	entrypoint := "main.py"
	if input.Language == "shell" {
		entrypoint = "main.sh"
	}
	if err = validateSandboxProgram(entrypoint, input.Code, input.Files); err != nil {
		return bad(400, "VALIDATION_ERROR", err.Error())
	}
	if err = os.MkdirAll(session.Workspace, 0777); err != nil {
		return err
	}
	_ = os.Chmod(session.Workspace, 0777)
	writeFile := func(name string, content []byte) error {
		path := filepath.Join(session.Workspace, name)
		if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
			return err
		}
		if err := os.WriteFile(path, content, 0666); err != nil {
			return err
		}
		return os.Chmod(path, 0666)
	}
	if err = writeFile(entrypoint, []byte(input.Code)); err != nil {
		return err
	}
	for _, file := range input.Files {
		if err = writeFile(file.Name, []byte(file.Content)); err != nil {
			return err
		}
	}
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return bad(503, "SANDBOX_RUNTIME_UNAVAILABLE", "未找到 docker 命令")
	}
	runtime := []string{"python3", "-I", "-S", "/work/main.py"}
	if input.Language == "shell" {
		runtime = []string{"/bin/sh", "/work/main.sh"}
	}
	args := append([]string{"exec", "--user", "65534:65534", "-w", "/work", session.ContainerID}, runtime...)
	runCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, dockerPath, args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"}
	stdout := &boundedBuffer{limit: sandboxOutputLimit}
	stderr := &boundedBuffer{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	result := sandboxResult{Stdout: stdout.String(), Stderr: stderr.String(), Files: map[string]string{}}
	if runErr != nil {
		if runCtx.Err() != nil {
			return bad(408, "SANDBOX_TIMEOUT", "沙箱执行超时")
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return bad(502, "SANDBOX_RUNTIME_FAILED", "沙箱执行失败")
		}
	}
	if err = checkSandboxWorkspace(session.Workspace); err != nil {
		return bad(413, "SANDBOX_RESOURCE_LIMIT", "沙箱工作区超过文件或空间限制")
	}
	collectSandboxFiles(session.Workspace, entrypoint, result.Files)
	_, _ = s.DB.ExecContext(r.Context(), `UPDATE sandbox_sessions SET last_used_at=UTC_TIMESTAMP(3),expires_at=DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 30 MINUTE) WHERE id=UNHEX(?)`, sessionID)
	write(w, 200, map[string]any{"stdout": result.Stdout, "stderr": result.Stderr, "exitCode": result.ExitCode, "files": result.Files})
	return nil
}

func (s *Server) stopSandboxSession(w http.ResponseWriter, r *http.Request, u User, sessionID string) error {
	session, err := s.sandboxSessionByID(r.Context(), u.ID, sessionID)
	if err == sql.ErrNoRows {
		return bad(404, "SANDBOX_SESSION_NOT_FOUND", "沙箱会话不存在")
	}
	if err != nil {
		return err
	}
	if err = s.stopSandboxSessionInternal(r.Context(), session); err != nil {
		return err
	}
	write(w, 200, map[string]any{"id": session.ID, "status": "stopped"})
	return nil
}

func (s *Server) stopSandboxSessionInternal(ctx context.Context, session sandboxSession) error {
	if session.ContainerID != "" {
		if dockerPath, err := exec.LookPath("docker"); err == nil {
			stopCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			_ = exec.CommandContext(stopCtx, dockerPath, "rm", "-f", session.ContainerID).Run()
			cancel()
		}
	}
	_ = os.RemoveAll(session.Workspace)
	_, err := s.DB.ExecContext(ctx, `UPDATE sandbox_sessions SET status='stopped',last_error='' WHERE id=UNHEX(?)`, session.ID)
	return err
}

func (s *Server) RunSandboxSessionCleaner(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		rows, err := s.DB.QueryContext(ctx, `SELECT HEX(id),HEX(user_id),runtime,container_id,workspace_path,status,expires_at
			FROM sandbox_sessions WHERE status='active' AND expires_at<=UTC_TIMESTAMP(3) LIMIT 20`)
		if err == nil {
			var expired []sandboxSession
			for rows.Next() {
				var session sandboxSession
				if rows.Scan(&session.ID, &session.UserID, &session.Runtime, &session.ContainerID, &session.Workspace, &session.Status, &session.ExpiresAt) == nil {
					expired = append(expired, session)
				}
			}
			rows.Close()
			for _, session := range expired {
				_ = s.stopSandboxSessionInternal(ctx, session)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
