package live

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestAccountDeletionRequiresOwnershipTransferAndPurgesPrivateData(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires migration 000015: %v", err)
	}
	defer server.DB.Close()
	userID, otherID, teamID, projectID, taskID := id(), id(), id(), id(), id()
	nativeDeviceID := id()
	conversationID, messageID, memoryID := id(), id(), id()
	accessToken := token()
	password := "account-delete-test-password"
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id IN (UNHEX(?),UNHEX(?))`, userID, otherID)
		server.DB.Exec(`DELETE FROM notification_deliveries WHERE recipient_user_id IN (UNHEX(?),UNHEX(?))`, userID, otherID)
		server.DB.Exec(`DELETE FROM notification_native_devices WHERE user_id IN (UNHEX(?),UNHEX(?))`, userID, otherID)
		server.DB.Exec(`DELETE FROM notification_preferences WHERE user_id IN (UNHEX(?),UNHEX(?))`, userID, otherID)
		server.DB.Exec(`DELETE FROM platform_roles WHERE user_id IN (UNHEX(?),UNHEX(?))`, userID, otherID)
		server.DB.Exec(`DELETE FROM task_comments WHERE task_id=UNHEX(?)`, taskID)
		server.DB.Exec(`DELETE FROM tasks WHERE id=UNHEX(?)`, taskID)
		server.DB.Exec(`DELETE FROM projects WHERE id=UNHEX(?)`, projectID)
		server.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM user_memories WHERE owner_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM conversations WHERE owner_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?))`, userID, otherID)
	}()
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "delete-"+userID+"@example.invalid", "Deletion Owner", passwordHash); err != nil {
		t.Fatal(err)
	}
	otherHash, _ := bcrypt.GenerateFromPassword([]byte("another-test-password"), bcrypt.DefaultCost)
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, otherID, "delete-"+otherID+"@example.invalid", "Remaining Owner", otherHash); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Delete Test Team',?,'active',UNHEX(?),'{}')`, teamID, "delete-team-"+teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UTC_TIMESTAMP(3)),(UNHEX(?),UNHEX(?),'team_admin','active',UTC_TIMESTAMP(3))`, teamID, userID, teamID, otherID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO projects(id,team_id,name,description,created_by) VALUES(UNHEX(?),UNHEX(?),'Retained Project','shared data',UNHEX(?))`, projectID, teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO tasks(id,team_id,project_id,title,description,created_by,assignee_id) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'Retained Task','shared data',UNHEX(?),UNHEX(?))`, taskID, teamID, projectID, userID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO task_comments(id,team_id,task_id,author_id,body) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),'shared comment')`, id(), teamID, taskID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),'personal','private conversation')`, conversationID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversation_messages(id,conversation_id,role,content) VALUES(UNHEX(?),UNHEX(?),'user','private message')`, messageID, conversationID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO user_memories(id,owner_user_id,category,content,source_message_id) VALUES(UNHEX(?),UNHEX(?),'profile','private memory',UNHEX(?))`, memoryID, userID, messageID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), userID, hash(accessToken), time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO notification_preferences(user_id,native_push_enabled) VALUES(UNHEX(?),1)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO notification_native_devices(id,user_id,platform,token_hash,encrypted_token,status) VALUES(UNHEX(?),UNHEX(?),'ios',?,X'01020304','active')`, nativeDeviceID, userID, hash("account-deletion-test-device-token")); err != nil {
		t.Fatal(err)
	}
	mfaKey := make([]byte, 32)
	if _, err = rand.Read(mfaKey); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MFA_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(mfaKey))
	mfaSecret, err := newMFASecret()
	if err != nil {
		t.Fatal(err)
	}
	encryptedMFA, err := encryptMFASecret(userID, mfaSecret)
	if err != nil {
		t.Fatal(err)
	}
	currentStep := time.Now().UTC().Unix() / 30
	if _, err = server.DB.Exec(`INSERT INTO user_mfa(user_id,secret_ciphertext,enabled_at,last_totp_step) VALUES(UNHEX(?),?,UTC_TIMESTAMP(3),?)`, userID, encryptedMFA, currentStep-1); err != nil {
		t.Fatal(err)
	}
	validMFA, err := totpAt(mfaSecret, time.Unix(currentStep*30, 0))
	if err != nil {
		t.Fatal(err)
	}
	request := func(currentPassword, confirmation, mfaCode string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]string{"currentPassword": currentPassword, "confirmation": confirmation, "mfaCode": mfaCode})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/me/account", strings.NewReader(string(payload)))
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}
	if got := request("wrong-password", "删除账号", validMFA); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(password, "DELETE", validMFA); got.Code != http.StatusBadRequest {
		t.Fatalf("confirmation phrase status=%d body=%s", got.Code, got.Body.String())
	}
	if got := request(password, "删除账号", ""); got.Code != http.StatusUnauthorized || !strings.Contains(got.Body.String(), "MFA_INVALID_CODE") {
		t.Fatalf("MFA bypass should fail: %d %s", got.Code, got.Body.String())
	}
	if got := request(password, "删除账号", "000000"); got.Code != http.StatusUnauthorized || !strings.Contains(got.Body.String(), "MFA_INVALID_CODE") {
		t.Fatalf("invalid MFA code should fail: %d %s", got.Code, got.Body.String())
	}
	if got := request(password, "删除账号", validMFA); got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "TEAM_OWNERSHIP_TRANSFER_REQUIRED") {
		t.Fatalf("owner deletion must be blocked: %d %s", got.Code, got.Body.String())
	}
	if _, err = server.DB.Exec(`UPDATE team_memberships SET role='team_owner' WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, otherID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`UPDATE team_memberships SET role='member' WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO platform_roles(user_id,role) VALUES(UNHEX(?),'super_admin')`, userID); err != nil {
		t.Fatal(err)
	}
	if got := request(password, "删除账号", validMFA); got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "LAST_SUPER_ADMIN") {
		t.Fatalf("last platform admin deletion must be blocked: %d %s", got.Code, got.Body.String())
	}
	if _, err = server.DB.Exec(`INSERT INTO platform_roles(user_id,role) VALUES(UNHEX(?),'super_admin')`, otherID); err != nil {
		t.Fatal(err)
	}
	deleted := request(password, "删除账号", validMFA)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"privateDataPurged":true`) {
		t.Fatalf("deletion failed: %d %s", deleted.Code, deleted.Body.String())
	}
	var accountEmail, displayName, status string
	var deletedAt sql.NullTime
	if err = server.DB.QueryRow(`SELECT email,display_name,status,deleted_at FROM users WHERE id=UNHEX(?)`, userID).Scan(&accountEmail, &displayName, &status, &deletedAt); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(accountEmail, "deleted+") || strings.Contains(accountEmail, "@example.invalid") || displayName != "已注销用户" || status != "disabled" || !deletedAt.Valid {
		t.Fatalf("account was not anonymized: email=%q name=%q status=%q deletedAt=%v", accountEmail, displayName, status, deletedAt.Valid)
	}
	var privateConversations, privateMessages, privateMemories, memberships, sessions, retainedTasks, unassignedTasks, retainedComments, auditRecords, nativeDevices, notificationPreferences int
	checks := []struct {
		query string
		args  []any
		dest  *int
	}{
		{`SELECT COUNT(*) FROM conversations WHERE owner_user_id=UNHEX(?)`, []any{userID}, &privateConversations},
		{`SELECT COUNT(*) FROM conversation_messages WHERE id=UNHEX(?)`, []any{messageID}, &privateMessages},
		{`SELECT COUNT(*) FROM user_memories WHERE owner_user_id=UNHEX(?)`, []any{userID}, &privateMemories},
		{`SELECT COUNT(*) FROM team_memberships WHERE user_id=UNHEX(?)`, []any{userID}, &memberships},
		{`SELECT COUNT(*) FROM auth_sessions WHERE user_id=UNHEX(?)`, []any{userID}, &sessions},
		{`SELECT COUNT(*) FROM notification_native_devices WHERE user_id=UNHEX(?)`, []any{userID}, &nativeDevices},
		{`SELECT COUNT(*) FROM notification_preferences WHERE user_id=UNHEX(?)`, []any{userID}, &notificationPreferences},
		{`SELECT COUNT(*) FROM tasks WHERE id=UNHEX(?)`, []any{taskID}, &retainedTasks},
		{`SELECT COUNT(*) FROM tasks WHERE id=UNHEX(?) AND assignee_id IS NULL`, []any{taskID}, &unassignedTasks},
		{`SELECT COUNT(*) FROM task_comments WHERE task_id=UNHEX(?) AND author_id=UNHEX(?)`, []any{taskID, userID}, &retainedComments},
		{`SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=UNHEX(?) AND action='user.account_deleted'`, []any{userID}, &auditRecords},
	}
	for _, check := range checks {
		if err = server.DB.QueryRow(check.query, check.args...).Scan(check.dest); err != nil {
			t.Fatal(err)
		}
	}
	if privateConversations+privateMessages+privateMemories+memberships+sessions+nativeDevices+notificationPreferences != 0 || retainedTasks != 1 || unassignedTasks != 1 || retainedComments != 1 || auditRecords != 1 {
		t.Fatalf("incorrect retention/purge: private=(%d,%d,%d) memberships=%d sessions=%d nativeDevices=%d preferences=%d tasks=%d unassigned=%d comments=%d audit=%d", privateConversations, privateMessages, privateMemories, memberships, sessions, nativeDevices, notificationPreferences, retainedTasks, unassignedTasks, retainedComments, auditRecords)
	}
	var anonymizedAuthor string
	if err = server.DB.QueryRow(`SELECT u.display_name FROM task_comments c JOIN users u ON u.id=c.author_id WHERE c.task_id=UNHEX(?)`, taskID).Scan(&anonymizedAuthor); err != nil || anonymizedAuthor != "已注销用户" {
		t.Fatalf("shared content author not anonymized: %q err=%v", anonymizedAuthor, err)
	}
	oldSession := httptest.NewRecorder()
	server.ServeHTTP(oldSession, withBearer(accessToken))
	if oldSession.Code != http.StatusUnauthorized {
		t.Fatalf("old session remained usable: %d", oldSession.Code)
	}
}

func withBearer(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}
