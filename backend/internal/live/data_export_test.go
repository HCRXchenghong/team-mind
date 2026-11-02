package live

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPersonalDataExportIsAuthenticatedAndScoped(t *testing.T) {
	workspaceKey := make([]byte, 32)
	if _, err := rand.Read(workspaceKey); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_WORKSPACE_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(workspaceKey))
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	ownerID, otherID, teamID, conversationID, messageID, memoryID, workspaceFileID := id(), id(), id(), id(), id(), id(), id()
	formerTeamConversationID, formerTeamMessageID := id(), id()
	ownerToken, otherToken := token(), token()
	defer func() {
		server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM user_memories WHERE owner_user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM agent_workspace_files WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM conversations WHERE owner_user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Close()
	}()
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, ownerID, "export-"+ownerID+"@example.invalid", "Export Owner", "test-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, otherID, "export-"+otherID+"@example.invalid", "Other Owner", "test-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),'personal','private export conversation')`, conversationID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversation_messages(id,conversation_id,role,content) VALUES(UNHEX(?),UNHEX(?),'user','my private message')`, messageID, conversationID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO user_memories(id,owner_user_id,category,content,source_message_id,confirmed_at) VALUES(UNHEX(?),UNHEX(?),'profile','my private memory',UNHEX(?),UTC_TIMESTAMP(3))`, memoryID, ownerID, messageID); err != nil {
		t.Fatal(err)
	}
	encryptedWorkspaceFile, err := encryptAgentWorkspaceFile(ownerID, workspaceFileID, "private.txt", "my private workspace file")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO agent_workspace_files(id,user_id,name,encrypted_content,content_size) VALUES(UNHEX(?),UNHEX(?),'private.txt',?,?)`, workspaceFileID, ownerID, encryptedWorkspaceFile, len("my private workspace file")); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Former Export Team',?,'active',UNHEX(?),'{}')`, teamID, "export-former-"+teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'member','left',UTC_TIMESTAMP(3))`, teamID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,team_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'team_context','former team conversation')`, formerTeamConversationID, ownerID, teamID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversation_messages(id,conversation_id,role,content) VALUES(UNHEX(?),UNHEX(?),'assistant','former team private context')`, formerTeamMessageID, formerTeamConversationID); err != nil {
		t.Fatal(err)
	}
	otherConversationID := id()
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),'personal','other person conversation')`, otherConversationID, otherID); err != nil {
		t.Fatal(err)
	}
	for _, session := range []struct{ userID, secret string }{{ownerID, ownerToken}, {otherID, otherToken}} {
		if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), session.userID, hash(session.secret), time.Now().UTC().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	unauthenticated := httptest.NewRecorder()
	server.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/me/data-export", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/data-export", nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("sensitive export is cacheable: %q", response.Header().Get("Cache-Control"))
	}
	if !strings.Contains(response.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("export response is not downloadable: %q", response.Header().Get("Content-Disposition"))
	}
	if strings.Contains(response.Body.String(), ownerToken) || strings.Contains(response.Body.String(), "test-hash") || strings.Contains(response.Body.String(), "other person conversation") || strings.Contains(response.Body.String(), "former team conversation") || strings.Contains(response.Body.String(), "former team private context") {
		t.Fatalf("export disclosed a credential or another user's data: %s", response.Body.String())
	}
	var envelope struct {
		Code string `json:"code"`
		Data struct {
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
			Conversations []map[string]any `json:"conversations"`
			Messages      []map[string]any `json:"conversationMessages"`
			Memories      []map[string]any `json:"personalMemories"`
			Sessions      []map[string]any `json:"sessions"`
			Workspace     []map[string]any `json:"personalAgentWorkspaceFiles"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != "OK" || !strings.EqualFold(envelope.Data.Account.ID, ownerID) {
		t.Fatalf("unexpected export account: %+v", envelope.Data.Account)
	}
	if len(envelope.Data.Conversations) != 1 || len(envelope.Data.Messages) != 1 || len(envelope.Data.Memories) != 1 || len(envelope.Data.Sessions) != 1 || len(envelope.Data.Workspace) != 1 || envelope.Data.Workspace[0]["content"] != "my private workspace file" {
		t.Fatalf("unexpected export data conversations=%d messages=%d memories=%d sessions=%d workspace=%+v", len(envelope.Data.Conversations), len(envelope.Data.Messages), len(envelope.Data.Memories), len(envelope.Data.Sessions), envelope.Data.Workspace)
	}
	var auditCount int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=UNHEX(?) AND action='user.data_exported'`, ownerID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected an audit record for the export, got %d", auditCount)
	}
}
