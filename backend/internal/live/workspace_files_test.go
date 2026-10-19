package live

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAgentWorkspaceFilesEncryptedCRUDAndUserIsolation(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	encodedKey := base64.StdEncoding.EncodeToString(key)
	t.Setenv("AGENT_WORKSPACE_ENCRYPTION_KEY", encodedKey)
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration requires migration 000016: %v", err)
	}
	ownerID, otherID := id(), id()
	ownerToken, otherToken := token(), token()
	t.Cleanup(func() {
		server.DB.Exec(`DELETE FROM agent_workspace_files WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Close()
	})
	for _, user := range []struct{ id, name, secret string }{
		{ownerID, "Workspace Owner", ownerToken},
		{otherID, "Workspace Other", otherToken},
	} {
		if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, user.id, "workspace-"+user.id+"@example.invalid", user.name, "test-only-hash"); err != nil {
			t.Fatal(err)
		}
		if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), user.id, hash(user.secret), time.Now().UTC().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	call := func(method, path, bearer, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}
	decodeID := func(response *httptest.ResponseRecorder) string {
		t.Helper()
		var envelope struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode response: %v (%s)", err, response.Body.String())
		}
		return envelope.Data.ID
	}
	content := "private workspace content"
	createBody, _ := json.Marshal(agentWorkspaceContent{Name: "notes/plan.md", Content: content})
	created := call(http.MethodPost, "/api/v1/me/agent/workspace/files", ownerToken, string(createBody))
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	fileID := decodeID(created)
	var encrypted []byte
	var storedSize int
	if err = server.DB.QueryRow(`SELECT encrypted_content,content_size FROM agent_workspace_files WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, fileID, ownerID).Scan(&encrypted, &storedSize); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), content) || storedSize != len(content) {
		t.Fatal("workspace content must be encrypted at rest and retain its UTF-8 byte length")
	}

	var restartedContent string
	if err = server.DB.QueryRow(`SELECT name,encrypted_content FROM agent_workspace_files WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, fileID, ownerID).Scan(new(string), &encrypted); err != nil {
		t.Fatal(err)
	}
	restartedContent, err = decryptAgentWorkspaceFile(ownerID, fileID, "notes/plan.md", encrypted)
	if err != nil || restartedContent != content {
		t.Fatalf("decrypt after reopening persisted DB record: content=%q error=%v", restartedContent, err)
	}

	var readEnvelope struct {
		Data agentWorkspaceContent `json:"data"`
	}
	read := call(http.MethodGet, "/api/v1/me/agent/workspace/files/"+fileID, ownerToken, "")
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &readEnvelope) != nil || readEnvelope.Data.Content != content {
		t.Fatalf("owner read failed: status=%d body=%s", read.Code, read.Body.String())
	}
	otherRead := call(http.MethodGet, "/api/v1/me/agent/workspace/files/"+fileID, otherToken, "")
	if otherRead.Code != http.StatusNotFound {
		t.Fatalf("another account read status=%d body=%s; want 404", otherRead.Code, otherRead.Body.String())
	}
	otherList := call(http.MethodGet, "/api/v1/me/agent/workspace/files", otherToken, "")
	if otherList.Code != http.StatusOK || strings.Contains(otherList.Body.String(), fileID) {
		t.Fatalf("another account list leaked file: status=%d body=%s", otherList.Code, otherList.Body.String())
	}

	updatedBody, _ := json.Marshal(agentWorkspaceContent{Name: "notes/plan.json", Content: `{"state":"updated"}`})
	updated := call(http.MethodPut, "/api/v1/me/agent/workspace/files/"+fileID, ownerToken, string(updatedBody))
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	read = call(http.MethodGet, "/api/v1/me/agent/workspace/files/"+fileID, ownerToken, "")
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &readEnvelope) != nil || readEnvelope.Data.Name != "notes/plan.json" || readEnvelope.Data.Content != `{"state":"updated"}` {
		t.Fatalf("updated content/name not readable: status=%d body=%s", read.Code, read.Body.String())
	}

	duplicate := call(http.MethodPost, "/api/v1/me/agent/workspace/files", ownerToken, string(updatedBody))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate filename status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	for _, name := range []string{"../escape.txt", "/absolute.md", "bad.exe"} {
		invalid, _ := json.Marshal(agentWorkspaceContent{Name: name, Content: "x"})
		response := call(http.MethodPost, "/api/v1/me/agent/workspace/files", ownerToken, string(invalid))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe filename %q status=%d body=%s", name, response.Code, response.Body.String())
		}
	}
	tooLarge, _ := json.Marshal(agentWorkspaceContent{Name: "too-large.txt", Content: strings.Repeat("x", agentWorkspaceFileBytes+1)})
	if response := call(http.MethodPost, "/api/v1/me/agent/workspace/files", ownerToken, string(tooLarge)); response.Code != http.StatusBadRequest {
		t.Fatalf("oversized file status=%d body=%s", response.Code, response.Body.String())
	}

	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=UNHEX(?) AND action LIKE 'agent.workspace.file.%'`, ownerID).Scan(&storedSize); err != nil {
		t.Fatal(err)
	}
	if storedSize != 2 {
		t.Fatalf("workspace writes should be audited, got %d audit rows", storedSize)
	}
	deleted := call(http.MethodDelete, "/api/v1/me/agent/workspace/files/"+fileID, ownerToken, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	missing := call(http.MethodGet, "/api/v1/me/agent/workspace/files/"+fileID, ownerToken, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted file still readable: status=%d body=%s", missing.Code, missing.Body.String())
	}

	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM agent_workspace_files WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID).Scan(&storedSize); err != nil {
		t.Fatal(err)
	}
	if storedSize != 0 {
		t.Fatalf("workspace deletion left %d rows", storedSize)
	}
}

func TestAgentWorkspaceFileNameValidation(t *testing.T) {
	valid := []string{"notes.md", "folder/plan.csv", "data.json", "nested/path/file.txt"}
	for _, name := range valid {
		if !validWorkspaceName(name) {
			t.Errorf("valid workspace name rejected: %q", name)
		}
	}
	invalid := []string{"", ".", "..", "../plan.txt", "folder/../plan.txt", "folder//plan.txt", "folder/", "file.exe", "a\\b.txt", "a\x00b.txt", "  "}
	for _, name := range invalid {
		if validWorkspaceName(name) {
			t.Errorf("unsafe workspace name accepted: %q", name)
		}
	}
}
