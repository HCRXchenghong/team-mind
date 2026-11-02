package live

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestEmailChangeVerificationLifecycle(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN and migration 000013: %v", err)
	}
	const clientIP = "198.51.100.213"
	const raceIP = "198.51.100.214"
	userID, holderID, lateHolderID := id(), id(), id()
	oldEmail := "email-change-" + userID + "@example.invalid"
	password := "email-change-password-2026"
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	oldSession, holderSession := token(), token()
	newSessionID, holderSessionID := id(), id()
	defer func() {
		for _, route := range []string{"email-change-request", "email-change-confirm"} {
			_, _ = server.DB.Exec(`DELETE FROM auth_ip_rate_limits WHERE scope_hash IN (?,?)`, loginScopeHash(server.loginRateKey, route, clientIP), loginScopeHash(server.loginRateKey, route, raceIP))
		}
		_, _ = server.DB.Exec(`DELETE FROM auth_ip_rate_limits WHERE scope_hash=?`, loginScopeHash(server.loginRateKey, "login", clientIP))
		_, _ = server.DB.Exec(`DELETE FROM email_change_requests WHERE user_id IN (UNHEX(?),UNHEX(?),UNHEX(?))`, userID, holderID, lateHolderID)
		_, _ = server.DB.Exec(`DELETE FROM password_resets WHERE user_id IN (UNHEX(?),UNHEX(?),UNHEX(?))`, userID, holderID, lateHolderID)
		_, _ = server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id IN (UNHEX(?),UNHEX(?),UNHEX(?))`, userID, holderID, lateHolderID)
		_, _ = server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id IN (UNHEX(?),UNHEX(?),UNHEX(?))`, userID, holderID, lateHolderID)
		_, _ = server.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?),UNHEX(?))`, userID, holderID, lateHolderID)
		_ = server.DB.Close()
	}()
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?),(UNHEX(?),?,?,?)`, userID, oldEmail, "Email Change User", string(passwordHash), holderID, "holder-"+holderID+"@example.invalid", "Email Holder", string(passwordHash)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?),(UNHEX(?),UNHEX(?),?,?)`, newSessionID, userID, hash(oldSession), time.Now().UTC().Add(time.Hour), holderSessionID, holderID, hash(holderSession), time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	call := func(method, path, bearer string, payload any, requestIPs ...string) *httptest.ResponseRecorder {
		var data []byte
		if payload != nil {
			data, _ = json.Marshal(payload)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		requestIP := clientIP
		if len(requestIPs) > 0 {
			requestIP = requestIPs[0]
		}
		req.RemoteAddr = requestIP + ":4100"
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}
	requestChange := func(email, currentPassword string) *httptest.ResponseRecorder {
		return call(http.MethodPost, "/api/v1/me/email-change", oldSession, map[string]string{"email": email, "currentPassword": currentPassword})
	}
	confirm := func(verificationToken string, requestIPs ...string) *httptest.ResponseRecorder {
		// No session is intentionally supplied: the mailbox token is the proof.
		return call(http.MethodPost, "/api/v1/me/email-change/confirm", "", map[string]string{"token": verificationToken}, requestIPs...)
	}
	decodeToken := func(response *httptest.ResponseRecorder) string {
		t.Helper()
		var envelope struct {
			Data struct {
				VerificationToken string `json:"verificationToken"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data.VerificationToken
	}

	wrongPassword := requestChange("wrong-"+userID+"@example.invalid", "not-the-password")
	if wrongPassword.Code != http.StatusUnauthorized || !strings.Contains(wrongPassword.Body.String(), "INVALID_CREDENTIALS") {
		t.Fatalf("wrong password should be rejected, status=%d body=%s", wrongPassword.Code, wrongPassword.Body.String())
	}
	initialConflict := requestChange("holder-"+holderID+"@example.invalid", password)
	if initialConflict.Code != http.StatusConflict || !strings.Contains(initialConflict.Body.String(), "EMAIL_EXISTS") {
		t.Fatalf("already used email should be rejected, status=%d body=%s", initialConflict.Code, initialConflict.Body.String())
	}

	// A race after issuing a request must still be detected at confirmation.
	conflictEmail := "late-holder-" + userID + "@example.invalid"
	conflictRequest := requestChange(conflictEmail, password)
	if conflictRequest.Code != http.StatusOK {
		t.Fatalf("request for available email failed: status=%d body=%s", conflictRequest.Code, conflictRequest.Body.String())
	}
	conflictToken := decodeToken(conflictRequest)
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, lateHolderID, conflictEmail, "Late Holder", string(passwordHash)); err != nil {
		t.Fatal(err)
	}
	lateConflict := confirm(conflictToken)
	if lateConflict.Code != http.StatusConflict || !strings.Contains(lateConflict.Body.String(), "EMAIL_EXISTS") {
		t.Fatalf("confirmation must recheck email uniqueness, status=%d body=%s", lateConflict.Code, lateConflict.Body.String())
	}

	expiringEmail := "expired-" + userID + "@example.invalid"
	expiringRequest := requestChange(expiringEmail, password)
	if expiringRequest.Code != http.StatusOK {
		t.Fatalf("expiring request failed: status=%d body=%s", expiringRequest.Code, expiringRequest.Body.String())
	}
	expiringToken := decodeToken(expiringRequest)
	if _, err = server.DB.Exec(`UPDATE email_change_requests SET expires_at=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 1 SECOND) WHERE token_hash=?`, hash(expiringToken)); err != nil {
		t.Fatal(err)
	}
	expired := confirm(expiringToken)
	if expired.Code != http.StatusBadRequest || !strings.Contains(expired.Body.String(), "EMAIL_CHANGE_INVALID") {
		t.Fatalf("expired token should be rejected, status=%d body=%s", expired.Code, expired.Body.String())
	}

	newEmail := "verified-" + userID + "@example.invalid"
	changeRequest := requestChange(newEmail, password)
	if changeRequest.Code != http.StatusOK {
		t.Fatalf("change request failed: status=%d body=%s", changeRequest.Code, changeRequest.Body.String())
	}
	verificationToken := decodeToken(changeRequest)
	if len(verificationToken) != 64 {
		t.Fatalf("expected 256-bit hex verification token, got %q", verificationToken)
	}
	var tokenHash []byte
	if err = server.DB.QueryRow(`SELECT token_hash FROM email_change_requests WHERE token_hash=?`, hash(verificationToken)).Scan(&tokenHash); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tokenHash, hash(verificationToken)) {
		t.Fatal("database does not contain the SHA-256 verification token hash")
	}
	changed := confirm(verificationToken)
	if changed.Code != http.StatusOK || !strings.Contains(changed.Body.String(), `"sessionsRevoked":true`) {
		t.Fatalf("valid email token failed without a browser session: status=%d body=%s", changed.Code, changed.Body.String())
	}
	var currentEmail string
	if err = server.DB.QueryRow(`SELECT email FROM users WHERE id=UNHEX(?)`, userID).Scan(&currentEmail); err != nil {
		t.Fatal(err)
	}
	if currentEmail != newEmail {
		t.Fatalf("email was not persisted: got %q want %q", currentEmail, newEmail)
	}
	var revokedAt sql.NullTime
	if err = server.DB.QueryRow(`SELECT revoked_at FROM auth_sessions WHERE id=UNHEX(?)`, newSessionID).Scan(&revokedAt); err != nil || !revokedAt.Valid {
		t.Fatalf("old session was not revoked: revokedAt=%v err=%v", revokedAt, err)
	}
	oldSessionResponse := call(http.MethodGet, "/api/v1/me", oldSession, nil)
	if oldSessionResponse.Code != http.StatusUnauthorized {
		t.Fatalf("old session remains usable: status=%d body=%s", oldSessionResponse.Code, oldSessionResponse.Body.String())
	}
	secondUse := confirm(verificationToken)
	if secondUse.Code != http.StatusBadRequest || !strings.Contains(secondUse.Body.String(), "EMAIL_CHANGE_INVALID") {
		t.Fatalf("verification token is reusable: status=%d body=%s", secondUse.Code, secondUse.Body.String())
	}
	login := call(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": newEmail, "password": password})
	if login.Code != http.StatusOK {
		t.Fatalf("new email cannot log in after confirmation: status=%d body=%s", login.Code, login.Body.String())
	}
	var loginEnvelope struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err = json.Unmarshal(login.Body.Bytes(), &loginEnvelope); err != nil || loginEnvelope.Data.AccessToken == "" {
		t.Fatalf("login response lacks access token: %v %s", err, login.Body.String())
	}
	concurrentEmail := "concurrent-" + userID + "@example.invalid"
	concurrentRequest := call(http.MethodPost, "/api/v1/me/email-change", loginEnvelope.Data.AccessToken, map[string]string{"email": concurrentEmail, "currentPassword": password}, raceIP)
	if concurrentRequest.Code != http.StatusOK {
		t.Fatalf("concurrent verification request failed: status=%d body=%s", concurrentRequest.Code, concurrentRequest.Body.String())
	}
	concurrentToken := decodeToken(concurrentRequest)
	responses := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() { responses <- confirm(concurrentToken, raceIP) }()
	}
	first, second := <-responses, <-responses
	if !((first.Code == http.StatusOK && second.Code == http.StatusBadRequest) || (second.Code == http.StatusOK && first.Code == http.StatusBadRequest)) {
		t.Fatalf("concurrent confirmation was not single-use: status/body=(%d,%s),(%d,%s)", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	var auditCount int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=UNHEX(?) AND action='user.email_changed'`, userID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("email change audit count=%d err=%v", auditCount, err)
	}
	var auditSummary string
	if err = server.DB.QueryRow(`SELECT CONCAT(COALESCE(before_summary,''),COALESCE(after_summary,'')) FROM audit_logs WHERE actor_user_id=UNHEX(?) AND action='user.email_changed' ORDER BY id DESC LIMIT 1`, userID).Scan(&auditSummary); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(auditSummary, newEmail) {
		t.Fatalf("audit log contains the full email address: %s", auditSummary)
	}
}
