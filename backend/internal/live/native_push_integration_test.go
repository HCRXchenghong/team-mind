package live

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativePushAPIIsolationAndDurableDelivery(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires migration 000018: %v", err)
	}
	ownerID, strangerID := id(), id()
	ownerSession, strangerSession := token(), token()
	cleanup := func() {
		_, _ = server.DB.Exec(`DELETE FROM notification_deliveries WHERE recipient_user_id IN (UNHEX(?),UNHEX(?))`, ownerID, strangerID)
		_, _ = server.DB.Exec(`DELETE FROM notifications WHERE recipient_user_id IN (UNHEX(?),UNHEX(?))`, ownerID, strangerID)
		_, _ = server.DB.Exec(`DELETE FROM notification_native_devices WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, strangerID)
		_, _ = server.DB.Exec(`DELETE FROM notification_preferences WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, strangerID)
		_, _ = server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, strangerID)
		_, _ = server.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?))`, ownerID, strangerID)
		_ = server.DB.Close()
	}
	t.Cleanup(cleanup)
	for _, user := range []struct{ id, email, session string }{{ownerID, "native-owner-" + ownerID + "@example.invalid", ownerSession}, {strangerID, "native-stranger-" + strangerID + "@example.invalid", strangerSession}} {
		if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, user.id, user.email, "Native Test", "not-a-login"); err != nil {
			t.Fatal(err)
		}
		if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), user.id, hash(user.session), time.Now().UTC().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, _ := x509.MarshalPKCS8PrivateKey(privateKey)
	serviceAccountFile := filepath.Join(t.TempDir(), "service-account.json")
	serviceAccount, _ := json.Marshal(firebaseServiceAccount{ProjectID: "native-test", ClientEmail: "teammind@test.iam.gserviceaccount.com", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}))})
	if err = os.WriteFile(serviceAccountFile, serviceAccount, 0o600); err != nil {
		t.Fatal(err)
	}
	encKey := make([]byte, 32)
	if _, err = rand.Read(encKey); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("FCM_SERVICE_ACCOUNT_FILE", serviceAccountFile)
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(encKey))

	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = w.Write([]byte(`{"access_token":"native-test-token","expires_in":3600}`))
			return
		}
		if r.URL.Path != "/projects/native-test/messages:send" || r.Header.Get("Authorization") != "Bearer native-test-token" {
			t.Errorf("unexpected FCM request path or auth: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		payload, readErr := io.ReadAll(r.Body)
		if readErr != nil || strings.Contains(string(payload), "secret task body") {
			t.Error("native push leaked notification body")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/native-test/messages/1"}`))
	}))
	defer pushServer.Close()
	server.firebase = newFirebaseMessagingSender()
	server.firebase.tokenURL = pushServer.URL + "/token"
	server.firebase.messageURL = func(project string) string { return pushServer.URL + "/projects/" + project + "/messages:send" }

	call := func(method, path, session string, payload any) *httptest.ResponseRecorder {
		var requestBody []byte
		if payload != nil {
			requestBody, _ = json.Marshal(payload)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(requestBody))
		req.RemoteAddr = "127.0.0.1:4123"
		req.Header.Set("Authorization", "Bearer "+session)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		return response
	}
	deviceToken := "fcm:abcdefghijklmnopqrstuvwxyz0123456789_-"
	registered := call(http.MethodPost, "/api/v1/me/notification-channels/native-devices", ownerSession, map[string]string{"platform": "android", "token": deviceToken})
	if registered.Code != http.StatusCreated {
		t.Fatalf("register device: %d %s", registered.Code, registered.Body.String())
	}
	if strings.Contains(registered.Body.String(), deviceToken) {
		t.Fatal("device registration response leaked the FCM token")
	}
	var registeredEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(registered.Body.Bytes(), &registeredEnvelope); err != nil || !validID(registeredEnvelope.Data.ID) {
		t.Fatalf("invalid register response: %s", registered.Body.String())
	}

	list := call(http.MethodGet, "/api/v1/me/notification-channels/native-devices", ownerSession, nil)
	var devicesEnvelope struct {
		Data []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err = json.Unmarshal(list.Body.Bytes(), &devicesEnvelope); err != nil {
		t.Fatal(err)
	}
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), deviceToken) || len(devicesEnvelope.Data) != 1 || !strings.EqualFold(devicesEnvelope.Data[0].ID, registeredEnvelope.Data.ID) {
		t.Fatalf("device listing leaked or omitted record: %d %s", list.Code, list.Body.String())
	}
	forgedRevoke := call(http.MethodDelete, "/api/v1/me/notification-channels/native-devices/"+registeredEnvelope.Data.ID, strangerSession, nil)
	if forgedRevoke.Code != http.StatusNotFound {
		t.Fatalf("cross-user device revoke status=%d body=%s", forgedRevoke.Code, forgedRevoke.Body.String())
	}

	preferences := call(http.MethodPatch, "/api/v1/me/notification-preferences", ownerSession, map[string]any{"enabled": true, "emailEnabled": false, "pushEnabled": false, "nativePushEnabled": true, "quietStart": "", "quietEnd": ""})
	if preferences.Code != http.StatusOK {
		t.Fatalf("enable native preference: %d %s", preferences.Code, preferences.Body.String())
	}
	tx, err := server.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = notifyResource(tx, "", ownerID, "native_test", "Generic task notification", "secret task body must never be pushed", "task", id()); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = server.processNotificationDeliveries(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = server.DB.QueryRow(`SELECT status FROM notification_deliveries WHERE recipient_user_id=UNHEX(?) AND channel='native_push' AND native_device_id=UNHEX(?)`, ownerID, registeredEnvelope.Data.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "delivered" {
		t.Fatalf("native delivery status=%s", status)
	}

	revoked := call(http.MethodDelete, "/api/v1/me/notification-channels/native-devices/"+registeredEnvelope.Data.ID, ownerSession, nil)
	if revoked.Code != http.StatusOK {
		t.Fatalf("owner revoke status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	if err = server.DB.QueryRow(`SELECT status FROM notification_native_devices WHERE id=UNHEX(?)`, registeredEnvelope.Data.ID).Scan(&status); err != nil || status != "revoked" {
		t.Fatalf("device status=%s err=%v", status, err)
	}
}
