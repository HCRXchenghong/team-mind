package live

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxRunsShellOfflineWithoutHostFilesOrEnvironment(t *testing.T) {
	if !agentShellAvailable() {
		t.Skip("the isolated Bash sandbox is unavailable")
	}
	secretName := "TEAMMIND_SANDBOX_TEST_SECRET"
	t.Setenv(secretName, "host-secret-must-not-leak")
	hostDir := t.TempDir()
	hostSecret := filepath.Join(hostDir, "host-secret.txt")
	if err := os.WriteFile(hostSecret, []byte("host-only"), 0600); err != nil {
		t.Fatal(err)
	}
	hostWrite := filepath.Join(hostDir, "host-write.txt")

	script := `set -eu
printf '%s' "$(cat input.txt)" > generated.txt
if [ -e "` + hostSecret + `" ]; then echo host-file-visible; else echo host-file-hidden; fi
if printf leaked > "` + hostWrite + `" 2>/dev/null; then echo host-write-succeeded; else echo host-write-blocked; fi
if [ -e /etc/passwd ]; then echo host-root-visible; else echo host-root-hidden; fi
if [ -w / ]; then echo root-writeable; else echo root-readonly; fi
if [ "${` + secretName + `-missing}" = "missing" ]; then echo secret-not-inherited; else echo secret-inherited; fi
/usr/bin/python3 -I -S -c 'import socket; s=socket.socket(); s.settimeout(.25); exec("try:\n s.connect((\"203.0.113.1\", 443))\n print(\"network-open\")\nexcept OSError:\n print(\"network-blocked\")")'
`
	result, err := runShellSandbox(context.Background(), script, []sandboxInputFile{{Name: "input.txt", Content: "selected input"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"host-file-hidden", "host-write-blocked", "host-root-hidden", "root-readonly", "secret-not-inherited", "network-blocked"} {
		if !strings.Contains(result.Stdout, expected) {
			t.Errorf("shell sandbox output lacks %q: stdout=%q stderr=%q exit=%d", expected, result.Stdout, result.Stderr, result.ExitCode)
		}
	}
	if result.ExitCode != 0 || result.Files["generated.txt"] != "selected input" {
		t.Fatalf("unexpected shell result: %+v", result)
	}
	if _, err = os.Stat(hostWrite); !os.IsNotExist(err) {
		t.Fatalf("sandbox wrote to host path %s: %v", hostWrite, err)
	}
}

func TestSandboxEntryPointCannotBeOverwrittenByInput(t *testing.T) {
	for _, entry := range []string{"main.py", "main.sh"} {
		if err := validateSandboxProgram(entry, "echo safe", []sandboxInputFile{{Name: entry, Content: "replace"}}); err == nil {
			t.Errorf("sandbox accepted input that overwrites %s", entry)
		}
	}
}
