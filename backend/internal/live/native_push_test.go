package live

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeDeviceTokenEncryptionBindsAccountAndHash(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	deviceToken := "fcm:abcdefghijklmnopqrstuvwxyz0123456789_-"
	deviceID, digest, ciphertext, err := encryptNativeDeviceToken("abcdef0123456789abcdef0123456789", deviceToken)
	if err != nil || !validID(deviceID) {
		t.Fatalf("encrypt failed: id=%q err=%v", deviceID, err)
	}
	if strings.Contains(string(ciphertext), deviceToken) {
		t.Fatal("device token was stored in clear text")
	}
	plain, err := decryptNativeDeviceToken("abcdef0123456789abcdef0123456789", digest, ciphertext)
	if err != nil || plain != deviceToken {
		t.Fatalf("decrypt=%q err=%v", plain, err)
	}
	if _, err = decryptNativeDeviceToken("00000000000000000000000000000000", digest, ciphertext); err == nil {
		t.Fatal("token decrypted for another user")
	}
	altered := append([]byte(nil), digest...)
	altered[0] ^= 1
	if _, err = decryptNativeDeviceToken("abcdef0123456789abcdef0123456789", altered, ciphertext); err == nil {
		t.Fatal("token decrypted with a different token hash")
	}
}

func TestNativeDeviceTokenValidation(t *testing.T) {
	for _, valid := range [][2]string{{"android", "fcm:abcdefghijklmnopqrstuvwxyz0123456789_-"}, {"ios", strings.Repeat("A", 200)}} {
		if err := validateNativeDeviceToken(valid[0], valid[1]); err != nil {
			t.Fatalf("valid token rejected: %v", err)
		}
	}
	for _, invalid := range [][2]string{{"web", strings.Repeat("a", 40)}, {"ios", "short"}, {"android", strings.Repeat("a", 30) + "?"}} {
		if err := validateNativeDeviceToken(invalid[0], invalid[1]); err == nil {
			t.Fatalf("invalid device token accepted: %q", invalid[0])
		}
	}
}

func TestFirebaseMessagingJWTAndFCMSend(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: func() []byte { raw, _ := x509.MarshalPKCS8PrivateKey(privateKey); return raw }()})
	directory := t.TempDir()
	serviceAccountPath := filepath.Join(directory, "service-account.json")
	serviceAccountJSON, _ := json.Marshal(firebaseServiceAccount{ProjectID: "team-test", ClientEmail: "teammind@test.iam.gserviceaccount.com", PrivateKey: string(privatePEM)})
	if err = os.WriteFile(serviceAccountPath, serviceAccountJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FCM_SERVICE_ACCOUNT_FILE", serviceAccountPath)
	t.Setenv("APP_ENV", "production")
	account, parsedKey, err := loadFirebaseServiceAccount()
	if err != nil || account.ProjectID != "team-test" || parsedKey.N.Cmp(privateKey.N) != 0 {
		t.Fatalf("load service account: project=%s err=%v", account.ProjectID, err)
	}
	if err = os.Chmod(serviceAccountPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err = loadFirebaseServiceAccount(); err == nil {
		t.Fatal("production accepted broadly readable service account file")
	}
	if err = os.Chmod(serviceAccountPath, 0o600); err != nil {
		t.Fatal(err)
	}

	var tokenRequests uint32
	var messageRequests uint32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			atomic.AddUint32(&tokenRequests, 1)
			if r.Method != http.MethodPost {
				t.Errorf("OAuth method=%s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse oauth form: %v", err)
				return
			}
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Error("unexpected grant type")
			}
			parts := strings.Split(r.Form.Get("assertion"), ".")
			if len(parts) != 3 {
				t.Error("service assertion is not a JWT")
				return
			}
			claimBytes, _ := base64.RawURLEncoding.DecodeString(parts[1])
			var claims map[string]any
			if json.Unmarshal(claimBytes, &claims) != nil || claims["iss"] != account.ClientEmail || claims["scope"] != firebaseMessagingScope || claims["aud"] != server.URL+"/token" {
				t.Errorf("unexpected service-account JWT claims: %v", claims)
			}
			signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
			digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if verifyErr := rsa.VerifyPKCS1v15(&privateKey.PublicKey, crypto.SHA256, digest[:], signature); verifyErr != nil {
				t.Errorf("JWT signature invalid: %v", verifyErr)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"test-access-token","expires_in":3600}`))
		case "/projects/team-test/messages:send":
			atomic.AddUint32(&messageRequests, 1)
			if r.Header.Get("Authorization") != "Bearer test-access-token" {
				t.Errorf("missing access token: %q", r.Header.Get("Authorization"))
			}
			var request struct {
				Message struct {
					Token        string            `json:"token"`
					Notification map[string]string `json:"notification"`
					Data         map[string]string `json:"data"`
				} `json:"message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode FCM request: %v", err)
			}
			if request.Message.Token != "a-valid-test-fcm-registration-token-1234567890" || request.Message.Data["route"] != "/teammind/notifications" {
				t.Errorf("unexpected FCM message: %+v", request.Message)
			}
			if strings.Contains(strings.Join([]string{request.Message.Notification["title"], request.Message.Notification["body"]}, " "), "private") {
				t.Error("FCM payload included private content")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"projects/team-test/messages/1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	sender := newFirebaseMessagingSender()
	sender.tokenURL = server.URL + "/token"
	sender.messageURL = func(project string) string { return server.URL + "/projects/" + project + "/messages:send" }
	for i := 0; i < 2; i++ {
		expired, sendErr := sender.send(context.Background(), account, parsedKey, "a-valid-test-fcm-registration-token-1234567890", map[string]string{"route": "/teammind/notifications"})
		if sendErr != nil || expired {
			t.Fatalf("send expired=%v err=%v", expired, sendErr)
		}
	}
	if atomic.LoadUint32(&tokenRequests) != 1 || atomic.LoadUint32(&messageRequests) != 2 {
		t.Fatalf("OAuth requests=%d FCM requests=%d", atomic.LoadUint32(&tokenRequests), atomic.LoadUint32(&messageRequests))
	}
}

func TestFirebaseMessagingRevokesUnregisteredTokens(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	account := firebaseServiceAccount{ProjectID: "test", ClientEmail: "svc@example.test"}
	var sendRequests uint32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"token","expires_in":3600}`))
			return
		}
		atomic.AddUint32(&sendRequests, 1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"status":"UNREGISTERED"}}`))
	}))
	defer server.Close()
	sender := newFirebaseMessagingSender()
	sender.tokenURL = server.URL + "/token"
	sender.messageURL = func(string) string { return server.URL + "/send" }
	expired, err := sender.send(context.Background(), account, privateKey, "expired-registration-token-1234567890", nil)
	if err == nil || !expired || atomic.LoadUint32(&sendRequests) != 1 {
		t.Fatalf("expired=%v err=%v requests=%d", expired, err, atomic.LoadUint32(&sendRequests))
	}
}

func TestFirebaseServiceAccountRejectsCustomOAuthEndpoint(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, _ := x509.MarshalPKCS8PrivateKey(key)
	path := filepath.Join(t.TempDir(), "account.json")
	encoded, _ := json.Marshal(firebaseServiceAccount{ProjectID: "test", ClientEmail: "svc@example.test", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})), TokenURI: "http://127.0.0.1:9000/token"})
	if err = os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FCM_SERVICE_ACCOUNT_FILE", path)
	t.Setenv("APP_ENV", "development")
	if _, _, err = loadFirebaseServiceAccount(); err == nil {
		t.Fatal("custom token URL could enable SSRF")
	}
}
