package live

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNotificationDeliveryDeduplicatesAndRecordsSuppression(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	userID, resourceID, suppressedResourceID := id(), id(), id()
	defer func() {
		server.DB.Exec(`DELETE FROM notification_deliveries WHERE recipient_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM notifications WHERE recipient_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM notification_preferences WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		server.DB.Close()
	}()
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "notify-"+userID+"@example.invalid", "Notify Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		tx, beginErr := server.DB.Begin()
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		if err = notifyResource(tx, "", userID, "test_event", "A test notification", "body", "task", resourceID); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var notifications, delivered int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_user_id=UNHEX(?)`, userID).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM notification_deliveries WHERE recipient_user_id=UNHEX(?) AND status='delivered'`, userID).Scan(&delivered); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 || delivered != 1 {
		t.Fatalf("duplicate notification counts inbox=%d delivered=%d", notifications, delivered)
	}
	if _, err = server.DB.Exec(`INSERT INTO notification_preferences(user_id,enabled) VALUES(UNHEX(?),0)`, userID); err != nil {
		t.Fatal(err)
	}
	tx, err := server.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = notifyResource(tx, "", userID, "test_event", "Suppressed test", "body", "task", suppressedResourceID); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	dedupeKey := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", userID, "test_event", "task", suppressedResourceID, "in_app", "Suppressed test", "body")))
	var status string
	if err = server.DB.QueryRow(`SELECT status FROM notification_deliveries WHERE idempotency_key=?`, dedupeKey[:]).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "suppressed" {
		t.Fatalf("disabled preference delivery status=%s", status)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_user_id=UNHEX(?)`, userID).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 {
		t.Fatalf("disabled preference created inbox notification: %d", notifications)
	}
}

func TestNotificationWorkerPersistsDeliveryAndRetries(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	userID, resourceID := id(), id()
	defer func() {
		server.DB.Exec(`DELETE FROM notification_deliveries WHERE recipient_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM notifications WHERE recipient_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM notification_push_subscriptions WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM notification_preferences WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		server.DB.Close()
	}()
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "notify-worker-"+userID+"@example.invalid", "Notify Worker Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	pushServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Errorf("invalid push request method/encoding: %s %s", r.Method, r.Header.Get("Content-Encoding"))
		}
		payload, readErr := io.ReadAll(r.Body)
		if readErr != nil || len(payload) == 0 || strings.Contains(string(payload), "private detail") {
			t.Errorf("invalid or unencrypted test payload: len=%d error=%v", len(payload), readErr)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer pushServer.Close()
	pushSub, _, _ := makeTestSubscription(t, pushServer.URL+"/push/test")
	rawSubscription, _ := json.Marshal(pushSub)
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encKey := make([]byte, 32)
	if _, err = rand.Read(encKey); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("VAPID_PRIVATE_KEY", base64.RawURLEncoding.EncodeToString(private.D.FillBytes(make([]byte, 32))))
	t.Setenv("VAPID_SUBJECT", "mailto:push-ops@team-mind.org")
	t.Setenv("NOTIFICATION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(encKey))
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1") // No listener: the durable worker must schedule a retry.
	t.Setenv("SMTP_USER", "test-user")
	t.Setenv("SMTP_PASSWORD", "test-password")
	t.Setenv("SMTP_FROM", "notifications@example.test")
	t.Setenv("PUBLIC_URL", "https://team.example.test")
	subscriptionID, endpointHash, encrypted, err := encryptPushSubscription(userID, pushSub.Endpoint, rawSubscription)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO notification_push_subscriptions(id,user_id,endpoint_hash,encrypted_subscription,status) VALUES(UNHEX(?),UNHEX(?),?,?, 'active')`, subscriptionID, userID, endpointHash, encrypted); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO notification_preferences(user_id,enabled,email_enabled,push_enabled) VALUES(UNHEX(?),1,1,1)`, userID); err != nil {
		t.Fatal(err)
	}
	tx, err := server.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = notifyResource(tx, "", userID, "test_event", "Generic notification", "private detail must not be sent", "task", resourceID); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = server.processNotificationDeliveries(context.Background()); err != nil {
		t.Fatal(err)
	}
	var emailStatus, emailReason, pushStatus string
	var attempts int
	if err = server.DB.QueryRow(`SELECT status,last_error,attempts FROM notification_deliveries WHERE recipient_user_id=UNHEX(?) AND channel='email' AND notification_type='test_event'`, userID).Scan(&emailStatus, &emailReason, &attempts); err != nil {
		t.Fatal(err)
	}
	if emailStatus != "retry" || emailReason != "email_unavailable" || attempts != 1 {
		t.Fatalf("email delivery status=%s reason=%s attempts=%d", emailStatus, emailReason, attempts)
	}
	if err = server.DB.QueryRow(`SELECT status FROM notification_deliveries WHERE recipient_user_id=UNHEX(?) AND channel='web_push' AND notification_type='test_event'`, userID).Scan(&pushStatus); err != nil {
		t.Fatal(err)
	}
	if pushStatus != "delivered" {
		t.Fatalf("push delivery status=%s", pushStatus)
	}
	var retryAt time.Time
	if err = server.DB.QueryRow(`SELECT next_retry_at FROM notification_deliveries WHERE recipient_user_id=UNHEX(?) AND channel='email' AND notification_type='test_event'`, userID).Scan(&retryAt); err != nil || retryAt.IsZero() {
		t.Fatalf("email retry was not scheduled: %v %v", retryAt, err)
	}
}
