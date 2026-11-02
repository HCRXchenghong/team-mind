package live

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestRFC6238TOTPVectors(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	tests := []struct {
		at   int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, test := range tests {
		got, err := totpAt(secret, time.Unix(test.at, 0))
		if err != nil || got != test.want {
			t.Errorf("TOTP at %d = %q, %v; want %q", test.at, got, err, test.want)
		}
	}
}

func TestMFASecretEncryptionIsBoundToUser(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	t.Setenv("MFA_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	secret := "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	ciphertext, err := encryptMFASecret("abcdef0123456789abcdef0123456789", secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte(secret)) {
		t.Fatal("TOTP secret appears in ciphertext")
	}
	decrypted, err := decryptMFASecret("abcdef0123456789abcdef0123456789", ciphertext)
	if err != nil || decrypted != secret {
		t.Fatalf("secret round trip failed: got=%q err=%v", decrypted, err)
	}
	if _, err = decryptMFASecret("00000000000000000000000000000000", ciphertext); err == nil {
		t.Fatal("ciphertext decrypted for another account")
	}
}

func TestMFAEnrollmentLoginRecoveryAndDisable(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN and migration 000014: %v", err)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(255 - i)
	}
	t.Setenv("MFA_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	const clientIP = "198.51.100.231"
	userID, sessionID := id(), id()
	userEmail := "mfa-" + userID + "@example.invalid"
	password := "mfa-integration-password"
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	firstSession := token()
	defer func() {
		for _, route := range []string{"mfa-setup", "mfa-enable", "mfa-disable", "mfa-complete", "login"} {
			_, _ = server.DB.Exec(`DELETE FROM auth_ip_rate_limits WHERE scope_hash=?`, loginScopeHash(server.loginRateKey, route, clientIP))
		}
		_, _ = server.DB.Exec(`DELETE FROM auth_mfa_challenges WHERE user_id=UNHEX(?)`, userID)
		_, _ = server.DB.Exec(`DELETE FROM mfa_recovery_codes WHERE user_id=UNHEX(?)`, userID)
		_, _ = server.DB.Exec(`DELETE FROM user_mfa WHERE user_id=UNHEX(?)`, userID)
		_, _ = server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=UNHEX(?)`, userID)
		_, _ = server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id=UNHEX(?)`, userID)
		_, _ = server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		_ = server.DB.Close()
	}()
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, userEmail, "MFA Test", string(passwordHash)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, sessionID, userID, hash(firstSession), time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	call := func(method, path, bearer string, payload any) *httptest.ResponseRecorder {
		var data []byte
		if payload != nil {
			data, _ = json.Marshal(payload)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.RemoteAddr = clientIP + ":4200"
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
	decode := func(response *httptest.ResponseRecorder, output any) {
		t.Helper()
		if err := json.Unmarshal(response.Body.Bytes(), output); err != nil {
			t.Fatalf("decode response: %v body=%s", err, response.Body.String())
		}
	}
	var setupEnvelope struct {
		Data struct {
			Secret     string `json:"secret"`
			OTPAuthURL string `json:"otpauthUrl"`
		} `json:"data"`
	}
	wrongPassword := call(http.MethodPost, "/api/v1/me/mfa/setup", firstSession, map[string]string{"currentPassword": "wrong-password"})
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("MFA setup should require current password: %d %s", wrongPassword.Code, wrongPassword.Body.String())
	}
	setup := call(http.MethodPost, "/api/v1/me/mfa/setup", firstSession, map[string]string{"currentPassword": password})
	if setup.Code != http.StatusOK {
		t.Fatalf("MFA setup failed: %d %s", setup.Code, setup.Body.String())
	}
	decode(setup, &setupEnvelope)
	secret := setupEnvelope.Data.Secret
	if secret == "" || !strings.HasPrefix(setupEnvelope.Data.OTPAuthURL, "otpauth://totp/TeamMind:") {
		t.Fatalf("invalid OTP setup response: %+v", setupEnvelope.Data)
	}
	var pendingCiphertext []byte
	if err = server.DB.QueryRow(`SELECT pending_secret_ciphertext FROM user_mfa WHERE user_id=UNHEX(?)`, userID).Scan(&pendingCiphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(pendingCiphertext, []byte(secret)) {
		t.Fatal("database contains the plaintext TOTP secret")
	}
	validCode, err := totpAt(secret, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	wrongCode := "000000"
	for verifyTOTP(secret, wrongCode, time.Now().UTC()) {
		code, _ := strconv.Atoi(wrongCode)
		wrongCode = fmt.Sprintf("%06d", (code+1)%1000000)
	}
	if verifyTOTP(secret, wrongCode, time.Now().UTC()) {
		t.Fatal("test selected a valid TOTP as the invalid code")
	}
	wrongSetupCode := call(http.MethodPost, "/api/v1/me/mfa/enable", firstSession, map[string]string{"code": wrongCode})
	if wrongSetupCode.Code != http.StatusBadRequest || !strings.Contains(wrongSetupCode.Body.String(), "MFA_INVALID_CODE") {
		t.Fatalf("wrong enrollment code should be rejected: %d %s", wrongSetupCode.Code, wrongSetupCode.Body.String())
	}
	validCode, _ = totpAt(secret, time.Now().UTC())
	enabled := call(http.MethodPost, "/api/v1/me/mfa/enable", firstSession, map[string]string{"code": validCode})
	if enabled.Code != http.StatusOK {
		t.Fatalf("MFA enable failed: %d %s", enabled.Code, enabled.Body.String())
	}
	var enabledEnvelope struct {
		Data struct {
			RecoveryCodes []string `json:"recoveryCodes"`
		} `json:"data"`
	}
	decode(enabled, &enabledEnvelope)
	recoveryCodes := enabledEnvelope.Data.RecoveryCodes
	if len(recoveryCodes) != mfaRecoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(recoveryCodes), mfaRecoveryCodeCount)
	}
	var enabledCiphertext []byte
	if err = server.DB.QueryRow(`SELECT secret_ciphertext FROM user_mfa WHERE user_id=UNHEX(?)`, userID).Scan(&enabledCiphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enabledCiphertext, []byte(secret)) {
		t.Fatal("database contains plaintext enabled TOTP secret")
	}
	var matchingRecoveryCount int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM mfa_recovery_codes WHERE user_id=UNHEX(?) AND code_hash=?`, userID, hash(normalizeRecoveryCode(recoveryCodes[0]))).Scan(&matchingRecoveryCount); err != nil {
		t.Fatal(err)
	}
	if matchingRecoveryCount != 1 {
		t.Fatal("recovery code is not stored as a one-way hash")
	}
	var statusEnvelope struct {
		Data struct {
			Enabled                bool `json:"enabled"`
			RemainingRecoveryCodes int  `json:"remainingRecoveryCodes"`
		} `json:"data"`
	}
	status := call(http.MethodGet, "/api/v1/me/mfa", firstSession, nil)
	if status.Code != http.StatusOK {
		t.Fatalf("MFA status failed: %d %s", status.Code, status.Body.String())
	}
	decode(status, &statusEnvelope)
	if !statusEnvelope.Data.Enabled || statusEnvelope.Data.RemainingRecoveryCodes != mfaRecoveryCodeCount {
		t.Fatalf("unexpected MFA status: %+v", statusEnvelope.Data)
	}

	type challengeResponse struct {
		Data struct {
			MFARequired    bool   `json:"mfaRequired"`
			ChallengeToken string `json:"challengeToken"`
			AccessToken    string `json:"accessToken"`
		} `json:"data"`
	}
	startLogin := func(remember bool) challengeResponse {
		response := call(http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": userEmail, "password": password, "remember": remember})
		if response.Code != http.StatusOK {
			t.Fatalf("password login failed: %d %s", response.Code, response.Body.String())
		}
		var result challengeResponse
		decode(response, &result)
		if !result.Data.MFARequired || result.Data.ChallengeToken == "" || result.Data.AccessToken != "" {
			t.Fatalf("password step created a session or omitted challenge: %+v", result.Data)
		}
		var hashStored []byte
		if err := server.DB.QueryRow(`SELECT token_hash FROM auth_mfa_challenges WHERE token_hash=?`, hash(result.Data.ChallengeToken)).Scan(&hashStored); err != nil || !bytes.Equal(hashStored, hash(result.Data.ChallengeToken)) {
			t.Fatalf("challenge token is not hash-stored: %v", err)
		}
		return result
	}
	complete := func(challengeToken, code string) *httptest.ResponseRecorder {
		return call(http.MethodPost, "/api/v1/auth/mfa/complete", "", map[string]string{"challengeToken": challengeToken, "code": code})
	}
	firstChallenge := startLogin(true)
	invalidLoginCode := complete(firstChallenge.Data.ChallengeToken, wrongCode)
	if invalidLoginCode.Code != http.StatusUnauthorized || !strings.Contains(invalidLoginCode.Body.String(), "MFA_INVALID_CODE") {
		t.Fatalf("wrong login TOTP should fail: %d %s", invalidLoginCode.Code, invalidLoginCode.Body.String())
	}
	var enabledStep int64
	if err = server.DB.QueryRow(`SELECT last_totp_step FROM user_mfa WHERE user_id=UNHEX(?)`, userID).Scan(&enabledStep); err != nil {
		t.Fatal(err)
	}
	validCode, _ = totpAt(secret, time.Unix((enabledStep+1)*30, 0))
	completeLogin := complete(firstChallenge.Data.ChallengeToken, validCode)
	if completeLogin.Code != http.StatusOK || !strings.Contains(completeLogin.Body.String(), "2592000") {
		t.Fatalf("valid TOTP login failed or remember choice lost: %d %s", completeLogin.Code, completeLogin.Body.String())
	}
	var loginEnvelope struct {
		Data struct {
			AccessToken string `json:"accessToken"`
			Remember    bool   `json:"remember"`
		} `json:"data"`
	}
	decode(completeLogin, &loginEnvelope)
	if loginEnvelope.Data.AccessToken == "" || !loginEnvelope.Data.Remember {
		t.Fatalf("completed MFA response missing session: %+v", loginEnvelope.Data)
	}

	currentStep := time.Now().UTC().Unix() / 30
	targetStep := currentStep
	if targetStep <= enabledStep+1 {
		targetStep = enabledStep + 2
	}
	if wait := time.Until(time.Unix(targetStep*30, 0).Add(100 * time.Millisecond)); targetStep > currentStep && wait > 0 {
		time.Sleep(wait)
	}
	concurrentCode, _ := totpAt(secret, time.Unix(targetStep*30, 0))
	concurrentChallenges := []challengeResponse{startLogin(false), startLogin(false)}
	concurrentResults := make(chan *httptest.ResponseRecorder, len(concurrentChallenges))
	var concurrent sync.WaitGroup
	for _, challenge := range concurrentChallenges {
		concurrent.Add(1)
		go func(challenge challengeResponse) {
			defer concurrent.Done()
			concurrentResults <- complete(challenge.Data.ChallengeToken, concurrentCode)
		}(challenge)
	}
	concurrent.Wait()
	close(concurrentResults)
	successes, replays := 0, 0
	for result := range concurrentResults {
		switch result.Code {
		case http.StatusOK:
			successes++
		case http.StatusUnauthorized:
			replays++
		default:
			t.Fatalf("unexpected concurrent TOTP result: %d %s", result.Code, result.Body.String())
		}
	}
	if successes != 1 || replays != 1 {
		t.Fatalf("concurrent TOTP replay protection: successes=%d replays=%d", successes, replays)
	}
	recoveryChallenge := startLogin(false)
	recoveryLogin := complete(recoveryChallenge.Data.ChallengeToken, recoveryCodes[0])
	if recoveryLogin.Code != http.StatusOK {
		t.Fatalf("recovery-code login failed: %d %s", recoveryLogin.Code, recoveryLogin.Body.String())
	}
	var consumed int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM mfa_recovery_codes WHERE user_id=UNHEX(?) AND used_at IS NOT NULL`, userID).Scan(&consumed); err != nil || consumed != 1 {
		t.Fatalf("recovery code was not consumed: count=%d err=%v", consumed, err)
	}
	replayChallenge := startLogin(false)
	replayedRecovery := complete(replayChallenge.Data.ChallengeToken, recoveryCodes[0])
	if replayedRecovery.Code != http.StatusUnauthorized {
		t.Fatalf("recovery code was reusable: %d %s", replayedRecovery.Code, replayedRecovery.Body.String())
	}

	disabled := call(http.MethodPost, "/api/v1/me/mfa/disable", loginEnvelope.Data.AccessToken, map[string]string{"currentPassword": password, "code": recoveryCodes[1]})
	if disabled.Code != http.StatusOK || !strings.Contains(disabled.Body.String(), `"sessionsRevoked":true`) {
		t.Fatalf("MFA disable failed: %d %s", disabled.Code, disabled.Body.String())
	}
	oldSession := call(http.MethodGet, "/api/v1/me", loginEnvelope.Data.AccessToken, nil)
	if oldSession.Code != http.StatusUnauthorized {
		t.Fatalf("MFA disable did not revoke all sessions: %d %s", oldSession.Code, oldSession.Body.String())
	}
	plainLogin := call(http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": userEmail, "password": password, "remember": false})
	var plainLoginEnvelope struct {
		Data struct {
			MFARequired bool   `json:"mfaRequired"`
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	decode(plainLogin, &plainLoginEnvelope)
	if plainLogin.Code != http.StatusOK || plainLoginEnvelope.Data.MFARequired || plainLoginEnvelope.Data.AccessToken == "" {
		t.Fatalf("disabled MFA still blocks password login: %d %+v %s", plainLogin.Code, plainLoginEnvelope.Data, plainLogin.Body.String())
	}
}
