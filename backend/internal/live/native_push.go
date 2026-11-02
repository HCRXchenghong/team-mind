package live

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const firebaseMessagingScope = "https://www.googleapis.com/auth/firebase.messaging"
const maxNativePushDevicesPerUser = 20

type firebaseServiceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

type firebaseMessagingSender struct {
	client       *http.Client
	tokenURL     string
	messageURL   func(string) string
	mu           sync.Mutex
	accessToken  string
	tokenExpires time.Time
}

func newFirebaseMessagingSender() *firebaseMessagingSender {
	return &firebaseMessagingSender{
		client:   &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		tokenURL: "https://oauth2.googleapis.com/token",
		messageURL: func(project string) string {
			return "https://fcm.googleapis.com/v1/projects/" + url.PathEscape(project) + "/messages:send"
		},
	}
}

func loadFirebaseServiceAccount() (firebaseServiceAccount, *rsa.PrivateKey, error) {
	var account firebaseServiceAccount
	path := strings.TrimSpace(os.Getenv("FCM_SERVICE_ACCOUNT_FILE"))
	if path == "" {
		return account, nil, errors.New("FCM service account is not configured")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 64*1024 {
		return account, nil, errors.New("FCM service account file is unavailable")
	}
	if os.Getenv("APP_ENV") == "production" && info.Mode().Perm()&0o077 != 0 {
		return account, nil, errors.New("FCM service account file permissions are too broad")
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &account) != nil {
		return account, nil, errors.New("FCM service account file is invalid")
	}
	account.ProjectID = strings.TrimSpace(account.ProjectID)
	account.ClientEmail = strings.TrimSpace(account.ClientEmail)
	if account.ProjectID == "" || len(account.ProjectID) > 200 || account.ClientEmail == "" || len(account.ClientEmail) > 320 {
		return account, nil, errors.New("FCM service account identity is invalid")
	}
	if account.TokenURI != "" && account.TokenURI != "https://oauth2.googleapis.com/token" {
		return account, nil, errors.New("FCM token endpoint is not allowed")
	}
	block, _ := pem.Decode([]byte(account.PrivateKey))
	if block == nil {
		return account, nil, errors.New("FCM service account key is invalid")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		parsed, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if err != nil || !ok || privateKey.N.BitLen() < 2048 {
		return account, nil, errors.New("FCM service account key is invalid")
	}
	return account, privateKey, nil
}

func nativePushConfigured() bool {
	_, _, err := loadFirebaseServiceAccount()
	if err != nil {
		return false
	}
	_, err = notificationEncryptionKey()
	return err == nil
}

func nativeDeviceTokenHash(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

func validateNativeDeviceToken(platform, token string) error {
	if platform != "android" && platform != "ios" {
		return errors.New("unsupported platform")
	}
	if len(token) < 20 || len(token) > 4096 {
		return errors.New("invalid registration token length")
	}
	for _, ch := range token {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-_:.", ch)) {
			return errors.New("invalid registration token")
		}
	}
	return nil
}

func encryptNativeDeviceToken(userID, rawToken string) (string, []byte, []byte, error) {
	key, err := notificationEncryptionKey()
	if err != nil {
		return "", nil, nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", nil, nil, err
	}
	digest := nativeDeviceTokenHash(rawToken)
	associated := append([]byte("teammind-native-push-v1\x00"+userID+"\x00"), digest...)
	ciphertext := gcm.Seal(nonce, nonce, []byte(rawToken), associated)
	return id(), digest, ciphertext, nil
}

func decryptNativeDeviceToken(userID string, digest, ciphertext []byte) (string, error) {
	key, err := notificationEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(ciphertext) < gcm.NonceSize() || len(digest) != sha256.Size {
		return "", errors.New("native push token ciphertext is invalid")
	}
	nonce, encrypted := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	associated := append([]byte("teammind-native-push-v1\x00"+userID+"\x00"), digest...)
	plain, err := gcm.Open(nil, nonce, encrypted, associated)
	if err != nil || !equalBytes(nativeDeviceTokenHash(string(plain)), digest) {
		return "", errors.New("native push token ciphertext is invalid")
	}
	return string(plain), nil
}

func (s *firebaseMessagingSender) accessTokenFor(ctx context.Context, account firebaseServiceAccount, privateKey *rsa.PrivateKey) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accessToken != "" && time.Until(s.tokenExpires) > time.Minute {
		return s.accessToken, nil
	}
	now := time.Now().UTC()
	claims, err := json.Marshal(map[string]any{"iss": account.ClientEmail, "scope": firebaseMessagingScope, "aud": s.tokenURL, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString(claims)
	unsigned := header + "." + payload
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	assertion := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("google oauth returned HTTP %d", response.StatusCode)
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 32*1024)).Decode(&tokenResponse); err != nil || tokenResponse.AccessToken == "" || tokenResponse.ExpiresIn < 60 {
		return "", errors.New("google oauth returned an invalid token response")
	}
	s.accessToken = tokenResponse.AccessToken
	s.tokenExpires = now.Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	return s.accessToken, nil
}

func (s *firebaseMessagingSender) send(ctx context.Context, account firebaseServiceAccount, privateKey *rsa.PrivateKey, registrationToken string, data map[string]string) (bool, error) {
	accessToken, err := s.accessTokenFor(ctx, account, privateKey)
	if err != nil {
		return false, err
	}
	encoded, err := json.Marshal(map[string]any{"message": map[string]any{
		"token":        registrationToken,
		"notification": map[string]string{"title": "TeamMind 新通知", "body": "你有一条新的通知，请登录查看。"},
		"data":         data,
		"android":      map[string]any{"priority": "high", "notification": map[string]string{"channel_id": "teammind_notifications"}},
		"apns":         map[string]any{"headers": map[string]string{"apns-priority": "10"}, "payload": map[string]any{"aps": map[string]any{"sound": "default"}}},
	}})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.messageURL(account.ProjectID), strings.NewReader(string(encoded)))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 32*1024))
	if readErr != nil {
		return false, readErr
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return false, nil
	}
	var apiError struct {
		Error struct {
			Status string `json:"status"`
		} `json:"error"`
	}
	_ = json.Unmarshal(responseBody, &apiError)
	return apiError.Error.Status == "UNREGISTERED" || response.StatusCode == http.StatusNotFound, fmt.Errorf("firebase messaging returned HTTP %d", response.StatusCode)
}

func (s *Server) nativeDevices(w http.ResponseWriter, r *http.Request, u User, path string) error {
	parts := strings.Split(path, "/")
	if len(parts) == 3 && r.Method == http.MethodGet {
		rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(id),platform,status,created_at,updated_at FROM notification_native_devices WHERE user_id=UNHEX(?) ORDER BY created_at DESC`, u.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		type nativeDevice struct {
			ID        string    `json:"id"`
			Platform  string    `json:"platform"`
			Status    string    `json:"status"`
			CreatedAt time.Time `json:"createdAt"`
			UpdatedAt time.Time `json:"updatedAt"`
		}
		devices := make([]nativeDevice, 0)
		for rows.Next() {
			var device nativeDevice
			if err = rows.Scan(&device.ID, &device.Platform, &device.Status, &device.CreatedAt, &device.UpdatedAt); err != nil {
				return err
			}
			devices = append(devices, device)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		write(w, http.StatusOK, devices)
		return nil
	}
	if len(parts) == 3 && r.Method == http.MethodPost {
		if !nativePushConfigured() {
			return bad(http.StatusServiceUnavailable, "NATIVE_PUSH_NOT_CONFIGURED", "原生推送服务尚未配置，请联系部署管理员")
		}
		var input struct {
			Platform string `json:"platform"`
			Token    string `json:"token"`
		}
		if err := body(w, r, &input); err != nil {
			return err
		}
		if err := validateNativeDeviceToken(input.Platform, input.Token); err != nil {
			return bad(http.StatusBadRequest, "INVALID_NATIVE_DEVICE", "设备推送信息无效")
		}
		deviceID, digest, encrypted, err := encryptNativeDeviceToken(u.ID, input.Token)
		if err != nil {
			return err
		}
		tx, err := s.DB.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var userStatus string
		if err = tx.QueryRowContext(r.Context(), `SELECT status FROM users WHERE id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&userStatus); err != nil {
			return err
		}
		if userStatus != "active" {
			return bad(http.StatusUnauthorized, "UNAUTHENTICATED", "账号当前不可用")
		}
		var existingID, existingUser, status, existingPlatform string
		err = tx.QueryRowContext(r.Context(), `SELECT HEX(id),HEX(user_id),status,platform FROM notification_native_devices WHERE token_hash=? FOR UPDATE`, digest).Scan(&existingID, &existingUser, &status, &existingPlatform)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && !strings.EqualFold(existingUser, u.ID) && status == "active" {
			return bad(http.StatusConflict, "NATIVE_DEVICE_IN_USE", "此设备推送令牌已绑定其他账号")
		}
		auditChange := err == sql.ErrNoRows || status != "active" || !strings.EqualFold(existingUser, u.ID) || existingPlatform != input.Platform
		if err == sql.ErrNoRows {
			var activeDevices int
			if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notification_native_devices WHERE user_id=UNHEX(?) AND status='active'`, u.ID).Scan(&activeDevices); err != nil {
				return err
			}
			if activeDevices >= maxNativePushDevicesPerUser {
				return bad(http.StatusConflict, "NATIVE_DEVICE_LIMIT", "此账号已达到原生推送设备数量上限")
			}
			_, err = tx.ExecContext(r.Context(), `INSERT INTO notification_native_devices(id,user_id,platform,token_hash,encrypted_token,status) VALUES(UNHEX(?),UNHEX(?),?,?,?,'active')`, deviceID, u.ID, input.Platform, digest, encrypted)
		} else {
			deviceID = existingID
			_, err = tx.ExecContext(r.Context(), `UPDATE notification_deliveries SET status='suppressed',last_error='device_reassigned',lease_until=NULL WHERE native_device_id=UNHEX(?) AND status IN ('queued','retry','sending')`, existingID)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), `UPDATE notification_native_devices SET user_id=UNHEX(?),platform=?,encrypted_token=?,status='active' WHERE id=UNHEX(?)`, u.ID, input.Platform, encrypted, existingID)
			}
			if err == nil && !strings.EqualFold(existingUser, u.ID) {
				_, err = tx.ExecContext(r.Context(), `UPDATE notification_preferences SET native_push_enabled=0 WHERE user_id=UNHEX(?) AND NOT EXISTS (SELECT 1 FROM notification_native_devices WHERE user_id=UNHEX(?) AND status='active' AND id<>UNHEX(?))`, existingUser, existingUser, existingID)
			}
		}
		if err != nil {
			return err
		}
		if auditChange {
			action := "user.native_push_device_registered"
			if status == "active" && existingUser != "" && !strings.EqualFold(existingUser, u.ID) {
				action = "user.native_push_device_reassigned"
			}
			if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),?,'native_push_device',UNHEX(?),JSON_OBJECT('platform',?,'status','active'))`, u.ID, action, deviceID, input.Platform); err != nil {
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		write(w, http.StatusCreated, map[string]string{"id": deviceID})
		return nil
	}
	if len(parts) == 4 && validID(parts[3]) && r.Method == http.MethodDelete {
		tx, err := s.DB.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		res, err := tx.ExecContext(r.Context(), `UPDATE notification_native_devices SET status='revoked' WHERE id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, parts[3], u.ID)
		if err != nil {
			return err
		}
		count, _ := res.RowsAffected()
		if count == 0 {
			return bad(http.StatusNotFound, "NOT_FOUND", "设备推送订阅不存在")
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE notification_deliveries SET status='suppressed',last_error='device_revoked',lease_until=NULL WHERE native_device_id=UNHEX(?) AND status IN ('queued','retry','sending')`, parts[3]); err != nil {
			return err
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE notification_preferences SET native_push_enabled=0 WHERE user_id=UNHEX(?) AND NOT EXISTS (SELECT 1 FROM notification_native_devices WHERE user_id=UNHEX(?) AND status='active')`, u.ID, u.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),'user.native_push_device_revoked','native_push_device',UNHEX(?),JSON_OBJECT('status','revoked'))`, u.ID, parts[3]); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		write(w, http.StatusOK, map[string]bool{"revoked": true})
		return nil
	}
	return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for i := range left {
		difference |= left[i] ^ right[i]
	}
	return difference == 0
}

func decodeNativeDeviceID(raw string) ([]byte, error) { return hex.DecodeString(raw) }
