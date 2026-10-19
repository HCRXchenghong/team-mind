package live

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const mfaRecoveryCodeCount = 10

func mfaEncryptionKey() ([]byte, error) {
	key, err := decodeBase64Key(os.Getenv("MFA_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("MFA_ENCRYPTION_KEY must be a base64 encoded 32 byte key")
	}
	return key, nil
}

func encryptMFASecret(userID, secret string) ([]byte, error) {
	key, err := mfaEncryptionKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(secret), []byte(strings.ToLower(userID))), nil
}

func decryptMFASecret(userID string, ciphertext []byte) (string, error) {
	key, err := mfaEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize()+gcm.Overhead() {
		return "", fmt.Errorf("invalid encrypted MFA secret")
	}
	nonce, sealed := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, sealed, []byte(strings.ToLower(userID)))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func newMFASecret() (string, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

func mfaOTPAuthURL(email, secret string) string {
	label := url.PathEscape("TeamMind:" + email)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", "TeamMind")
	query.Set("algorithm", "SHA1")
	query.Set("digits", "6")
	query.Set("period", "30")
	return "otpauth://totp/" + label + "?" + query.Encode()
}

func totpAt(secret string, at time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) < 16 {
		return "", fmt.Errorf("invalid TOTP secret")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	value := (uint32(digest[offset])&0x7f)<<24 | uint32(digest[offset+1])<<16 | uint32(digest[offset+2])<<8 | uint32(digest[offset+3])
	return fmt.Sprintf("%06d", value%1000000), nil
}

func verifyTOTP(secret, code string, now time.Time) bool {
	_, valid := matchingTOTPStep(secret, code, now)
	return valid
}

func matchingTOTPStep(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	currentStep := now.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		want, err := totpAt(secret, now.Add(time.Duration(offset)*30*time.Second))
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return currentStep + offset, true
		}
	}
	return 0, false
}

func normalizeRecoveryCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	return strings.ReplaceAll(code, " ", "")
}

func newRecoveryCode() (display, normalized string, err error) {
	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	normalized = hex.EncodeToString(raw)
	parts := []string{normalized[:8], normalized[8:16], normalized[16:24], normalized[24:]}
	return strings.Join(parts, "-"), normalized, nil
}

func verifyRecoveryCode(code string) bool {
	code = normalizeRecoveryCode(code)
	if len(code) != 32 {
		return false
	}
	_, err := hex.DecodeString(code)
	return err == nil
}

func (s *Server) mfaRoute(w http.ResponseWriter, r *http.Request, user User, path string) error {
	switch path {
	case "me/mfa":
		if err := method(r, http.MethodGet); err != nil {
			return err
		}
		var enabled bool
		var recoveryCodes int
		if err := s.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=UNHEX(?) AND enabled_at IS NOT NULL), (SELECT COUNT(*) FROM mfa_recovery_codes WHERE user_id=UNHEX(?) AND used_at IS NULL)`, user.ID, user.ID).Scan(&enabled, &recoveryCodes); err != nil {
			return err
		}
		_, keyErr := mfaEncryptionKey()
		write(w, http.StatusOK, map[string]any{"enabled": enabled, "available": keyErr == nil, "remainingRecoveryCodes": recoveryCodes})
		return nil
	case "me/mfa/setup":
		if r.Method == http.MethodDelete {
			return s.cancelMFASetup(w, r, user)
		}
		return s.beginMFASetup(w, r, user)
	case "me/mfa/enable":
		return s.enableMFA(w, r, user)
	case "me/mfa/disable":
		return s.disableMFA(w, r, user)
	default:
		return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
	}
}

func (s *Server) cancelMFASetup(w http.ResponseWriter, r *http.Request, user User) error {
	if err := method(r, http.MethodDelete); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled sql.NullTime
	err = tx.QueryRowContext(r.Context(), `SELECT enabled_at FROM user_mfa WHERE user_id=UNHEX(?) FOR UPDATE`, user.ID).Scan(&enabled)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if enabled.Valid {
		return bad(http.StatusConflict, "MFA_ALREADY_ENABLED", "多因素认证已启用，不能取消待设置流程")
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM user_mfa WHERE user_id=UNHEX(?)`, user.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),'user.mfa_setup_cancelled','user',UNHEX(?),JSON_OBJECT('method','totp'))`, user.ID, user.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]bool{"cancelled": true})
	return nil
}

func (s *Server) beginMFASetup(w http.ResponseWriter, r *http.Request, user User) error {
	if err := method(r, http.MethodPost); err != nil {
		return err
	}
	if err := s.enforceRequestRateLimit(r, "mfa-setup"); err != nil {
		return err
	}
	if _, err := mfaEncryptionKey(); err != nil {
		return bad(http.StatusServiceUnavailable, "MFA_NOT_CONFIGURED", "多因素认证密钥未配置，请联系管理员")
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	if input.CurrentPassword == "" || len(input.CurrentPassword) > 72 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请输入当前密码")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var passwordHash, status string
	if err = tx.QueryRowContext(r.Context(), `SELECT password_hash,status FROM users WHERE id=UNHEX(?) FOR UPDATE`, user.ID).Scan(&passwordHash, &status); err != nil {
		return err
	}
	if status != "active" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.CurrentPassword)) != nil {
		return bad(http.StatusUnauthorized, "INVALID_CREDENTIALS", "当前密码错误")
	}
	var enabled bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=UNHEX(?) AND enabled_at IS NOT NULL)`, user.ID).Scan(&enabled); err != nil {
		return err
	}
	if enabled {
		return bad(http.StatusConflict, "MFA_ALREADY_ENABLED", "多因素认证已启用，请验证后关闭")
	}
	secret, err := newMFASecret()
	if err != nil {
		return err
	}
	ciphertext, err := encryptMFASecret(user.ID, secret)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO user_mfa(user_id,pending_secret_ciphertext,pending_expires_at) VALUES(UNHEX(?),?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 10 MINUTE)) ON DUPLICATE KEY UPDATE pending_secret_ciphertext=VALUES(pending_secret_ciphertext),pending_expires_at=VALUES(pending_expires_at)`, user.ID, ciphertext); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),'user.mfa_setup_started','user',UNHEX(?),JSON_OBJECT('method','totp'))`, user.ID, user.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]any{"secret": secret, "otpauthUrl": mfaOTPAuthURL(user.Email, secret), "expiresIn": 600})
	return nil
}

func (s *Server) enableMFA(w http.ResponseWriter, r *http.Request, user User) error {
	if err := method(r, http.MethodPost); err != nil {
		return err
	}
	if err := s.enforceRequestRateLimit(r, "mfa-enable"); err != nil {
		return err
	}
	var input struct {
		Code string `json:"code"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var encrypted []byte
	var pendingExpiry sql.NullTime
	var enabled sql.NullTime
	var lastTOTPStep sql.NullInt64
	if err = tx.QueryRowContext(r.Context(), `SELECT pending_secret_ciphertext,pending_expires_at,enabled_at,last_totp_step FROM user_mfa WHERE user_id=UNHEX(?) FOR UPDATE`, user.ID).Scan(&encrypted, &pendingExpiry, &enabled, &lastTOTPStep); err != nil {
		if err == sql.ErrNoRows {
			return bad(http.StatusConflict, "MFA_SETUP_REQUIRED", "请先开始多因素认证设置")
		}
		return err
	}
	if enabled.Valid {
		return bad(http.StatusConflict, "MFA_ALREADY_ENABLED", "多因素认证已经启用")
	}
	if !pendingExpiry.Valid || !pendingExpiry.Time.After(time.Now().UTC()) || len(encrypted) == 0 {
		return bad(http.StatusConflict, "MFA_SETUP_EXPIRED", "设置已过期，请重新开始")
	}
	secret, err := decryptMFASecret(user.ID, encrypted)
	if err != nil {
		return err
	}
	acceptedStep, validCode := matchingTOTPStep(secret, input.Code, time.Now().UTC())
	if !validCode || lastTOTPStep.Valid && acceptedStep <= lastTOTPStep.Int64 {
		return bad(http.StatusBadRequest, "MFA_INVALID_CODE", "验证码错误，请检查身份验证器中的最新验证码")
	}
	recoveryCodes := make([]string, 0, mfaRecoveryCodeCount)
	for i := 0; i < mfaRecoveryCodeCount; i++ {
		display, normalized, codeErr := newRecoveryCode()
		if codeErr != nil {
			return codeErr
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO mfa_recovery_codes(id,user_id,code_hash) VALUES(UNHEX(?),UNHEX(?),?)`, id(), user.ID, hash(normalized)); err != nil {
			return err
		}
		recoveryCodes = append(recoveryCodes, display)
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE user_mfa SET secret_ciphertext=pending_secret_ciphertext,pending_secret_ciphertext=NULL,pending_expires_at=NULL,enabled_at=UTC_TIMESTAMP(3),last_totp_step=? WHERE user_id=UNHEX(?)`, acceptedStep, user.ID); err != nil {
		return err
	}
	currentToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if _, err = tx.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND revoked_at IS NULL AND token_hash<>?`, user.ID, hash(currentToken)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),'user.mfa_enabled','user',UNHEX(?),JSON_OBJECT('method','totp','recoveryCodes',?))`, user.ID, user.ID, mfaRecoveryCodeCount); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]any{"enabled": true, "recoveryCodes": recoveryCodes})
	return nil
}

func (s *Server) disableMFA(w http.ResponseWriter, r *http.Request, user User) error {
	if err := method(r, http.MethodPost); err != nil {
		return err
	}
	if err := s.enforceRequestRateLimit(r, "mfa-disable"); err != nil {
		return err
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		Code            string `json:"code"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var passwordHash, status string
	if err = tx.QueryRowContext(r.Context(), `SELECT password_hash,status FROM users WHERE id=UNHEX(?) FOR UPDATE`, user.ID).Scan(&passwordHash, &status); err != nil {
		return err
	}
	if status != "active" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.CurrentPassword)) != nil {
		return bad(http.StatusUnauthorized, "INVALID_CREDENTIALS", "当前密码错误")
	}
	var encrypted []byte
	var enabled sql.NullTime
	var lastTOTPStep sql.NullInt64
	if err = tx.QueryRowContext(r.Context(), `SELECT secret_ciphertext,enabled_at,last_totp_step FROM user_mfa WHERE user_id=UNHEX(?) FOR UPDATE`, user.ID).Scan(&encrypted, &enabled, &lastTOTPStep); err != nil {
		if err == sql.ErrNoRows {
			return bad(http.StatusConflict, "MFA_NOT_ENABLED", "多因素认证尚未启用")
		}
		return err
	}
	if !enabled.Valid || len(encrypted) == 0 {
		return bad(http.StatusConflict, "MFA_NOT_ENABLED", "多因素认证尚未启用")
	}
	secret, err := decryptMFASecret(user.ID, encrypted)
	if err != nil {
		return err
	}
	acceptedStep, valid := matchingTOTPStep(secret, input.Code, time.Now().UTC())
	if valid && lastTOTPStep.Valid && acceptedStep <= lastTOTPStep.Int64 {
		valid = false
	}
	usedTOTP := valid
	if !valid && verifyRecoveryCode(input.Code) {
		res, updateErr := tx.ExecContext(r.Context(), `UPDATE mfa_recovery_codes SET used_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND code_hash=? AND used_at IS NULL`, user.ID, hash(normalizeRecoveryCode(input.Code)))
		if updateErr != nil {
			return updateErr
		}
		rows, _ := res.RowsAffected()
		valid = rows == 1
	}
	if !valid {
		return bad(http.StatusUnauthorized, "MFA_INVALID_CODE", "动态验证码或恢复码错误")
	}
	if usedTOTP {
		if _, err = tx.ExecContext(r.Context(), `UPDATE user_mfa SET last_totp_step=? WHERE user_id=UNHEX(?)`, acceptedStep, user.ID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM mfa_recovery_codes WHERE user_id=UNHEX(?)`, user.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM user_mfa WHERE user_id=UNHEX(?)`, user.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE auth_mfa_challenges SET consumed_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND consumed_at IS NULL`, user.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND revoked_at IS NULL`, user.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),'user.mfa_disabled','user',UNHEX(?),JSON_OBJECT('method','totp'))`, user.ID, user.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]any{"enabled": false, "sessionsRevoked": true})
	return nil
}

func (s *Server) completeMFALogin(w http.ResponseWriter, r *http.Request) error {
	if err := method(r, http.MethodPost); err != nil {
		return err
	}
	var input struct {
		ChallengeToken string `json:"challengeToken"`
		Code           string `json:"code"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	if len(input.ChallengeToken) != 64 || len(input.Code) > 64 {
		return bad(http.StatusBadRequest, "MFA_CHALLENGE_INVALID", "验证请求无效或已过期")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID string
	if err = tx.QueryRowContext(r.Context(), `SELECT LOWER(HEX(c.user_id)) FROM auth_mfa_challenges c JOIN user_mfa m ON m.user_id=c.user_id WHERE c.token_hash=? AND c.consumed_at IS NULL AND c.expires_at>UTC_TIMESTAMP(3) AND c.attempts<5 AND m.enabled_at IS NOT NULL`, hash(input.ChallengeToken)).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return bad(http.StatusUnauthorized, "MFA_CHALLENGE_INVALID", "验证请求无效或已过期，请重新登录")
		}
		return err
	}
	var status string
	if err = tx.QueryRowContext(r.Context(), `SELECT status FROM users WHERE id=UNHEX(?) FOR UPDATE`, userID).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return bad(http.StatusUnauthorized, "ACCOUNT_INACTIVE", "账号当前不可登录")
	}
	var remember bool
	if err = tx.QueryRowContext(r.Context(), `SELECT remember_login FROM auth_mfa_challenges WHERE token_hash=? AND user_id=UNHEX(?) AND consumed_at IS NULL AND expires_at>UTC_TIMESTAMP(3) AND attempts<5 FOR UPDATE`, hash(input.ChallengeToken), userID).Scan(&remember); err != nil {
		if err == sql.ErrNoRows {
			return bad(http.StatusUnauthorized, "MFA_CHALLENGE_INVALID", "验证请求无效或已过期，请重新登录")
		}
		return err
	}
	var encrypted []byte
	var lastTOTPStep sql.NullInt64
	if err = tx.QueryRowContext(r.Context(), `SELECT secret_ciphertext,last_totp_step FROM user_mfa WHERE user_id=UNHEX(?) AND enabled_at IS NOT NULL FOR UPDATE`, userID).Scan(&encrypted, &lastTOTPStep); err != nil {
		return err
	}
	secret, err := decryptMFASecret(userID, encrypted)
	if err != nil {
		return err
	}
	acceptedStep, valid := matchingTOTPStep(secret, input.Code, time.Now().UTC())
	if valid && lastTOTPStep.Valid && acceptedStep <= lastTOTPStep.Int64 {
		valid = false
	}
	usedTOTP := valid
	if !valid && verifyRecoveryCode(input.Code) {
		res, updateErr := tx.ExecContext(r.Context(), `UPDATE mfa_recovery_codes SET used_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND code_hash=? AND used_at IS NULL`, userID, hash(normalizeRecoveryCode(input.Code)))
		if updateErr != nil {
			return updateErr
		}
		rows, _ := res.RowsAffected()
		valid = rows == 1
	}
	if !valid {
		if _, err = tx.ExecContext(r.Context(), `UPDATE auth_mfa_challenges SET attempts=attempts+1 WHERE token_hash=? AND attempts<5`, hash(input.ChallengeToken)); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return bad(http.StatusUnauthorized, "MFA_INVALID_CODE", "动态验证码或恢复码错误")
	}
	if usedTOTP {
		if _, err = tx.ExecContext(r.Context(), `UPDATE user_mfa SET last_totp_step=? WHERE user_id=UNHEX(?)`, acceptedStep, userID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE auth_mfa_challenges SET consumed_at=UTC_TIMESTAMP(3) WHERE token_hash=? AND consumed_at IS NULL`, hash(input.ChallengeToken)); err != nil {
		return err
	}
	loginToken := token()
	duration := 8 * time.Hour
	if remember {
		duration = 30 * 24 * time.Hour
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), userID, hash(loginToken), time.Now().UTC().Add(duration)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]any{"accessToken": loginToken, "expiresIn": int(duration.Seconds()), "remember": remember})
	return nil
}
