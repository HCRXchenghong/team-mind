package live

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestRetentionPolicyPrunesExpiredRecordsAndPreservesRecentHistory(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires migration 000015: %v", err)
	}
	defer server.DB.Close()
	userID, oldNotificationID, newNotificationID, channelID := id(), id(), id(), id()
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("retention-test-password"), bcrypt.DefaultCost)
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "retention-"+userID+"@example.invalid", "Retention Test", passwordHash); err != nil {
		t.Fatal(err)
	}
	defer func() {
		server.DB.Exec(`DELETE FROM notification_deliveries WHERE recipient_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM notification_native_devices WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM notifications WHERE recipient_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM scheduled_jobs WHERE owner_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM password_resets WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM email_change_requests WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM auth_mfa_challenges WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?)`, channelID)
		server.DB.Exec(`DELETE FROM channel_accounts WHERE id=UNHEX(?)`, channelID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
	}()
	old := time.Now().UTC().AddDate(0, 0, -40)
	recent := time.Now().UTC().AddDate(0, 0, -2)
	oldSession := time.Now().UTC().AddDate(0, 0, -120)
	oldReminder := time.Now().UTC().AddDate(0, 0, -120)
	oldNotification := time.Now().UTC().AddDate(0, 0, -400)
	oldAudit := time.Now().UTC().AddDate(-8, 0, 0)
	oldFutureExpiry := time.Now().UTC().AddDate(0, 0, 2)
	oldPastExpiry := old.Add(-time.Hour)
	resetID := id()
	if _, err = server.DB.Exec(`INSERT INTO password_resets(id,user_id,token_hash,expires_at,created_at) VALUES(UNHEX(?),UNHEX(?),?,?,?)`, resetID, userID, hash(token()), oldPastExpiry, old); err != nil {
		t.Fatal(err)
	}
	activeResetID := id()
	if _, err = server.DB.Exec(`INSERT INTO password_resets(id,user_id,token_hash,expires_at,created_at) VALUES(UNHEX(?),UNHEX(?),?,?,?)`, activeResetID, userID, hash(token()), oldFutureExpiry, old); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO email_change_requests(id,user_id,new_email,token_hash,expires_at,consumed_at,created_at) VALUES(UNHEX(?),UNHEX(?),'old@example.invalid',?,?,UTC_TIMESTAMP(3),?)`, id(), userID, hash(token()), oldFutureExpiry, old); err != nil {
		t.Fatal(err)
	}
	challengeID := id()
	if _, err = server.DB.Exec(`INSERT INTO auth_mfa_challenges(id,user_id,token_hash,expires_at,consumed_at,created_at) VALUES(UNHEX(?),UNHEX(?),?,?,UTC_TIMESTAMP(3),?)`, challengeID, userID, hash(token()), oldFutureExpiry, old); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at,revoked_at,created_at) VALUES(UNHEX(?),UNHEX(?),?,?,UTC_TIMESTAMP(3),?)`, id(), userID, hash(token()), oldSession.Add(time.Hour), oldSession); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id string
		at time.Time
	}{{oldNotificationID, oldNotification}, {newNotificationID, recent}} {
		if _, err = server.DB.Exec(`INSERT INTO notifications(id,recipient_user_id,type,title,body,created_at) VALUES(UNHEX(?),UNHEX(?),'test','retention test','content',?)`, item.id, userID, item.at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO notification_deliveries(id,notification_id,recipient_user_id,notification_type,channel,status,idempotency_key,created_at) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'test','in_app','delivered',?,?),(UNHEX(?),UNHEX(?),UNHEX(?),'test','in_app','delivered',?,?)`, id(), oldNotificationID, userID, hash(token()), oldNotification, id(), newNotificationID, userID, hash(token()), recent); err != nil {
		t.Fatal(err)
	}
	oldRevokedDevice, oldActiveDevice := id(), id()
	if _, err = server.DB.Exec(`INSERT INTO notification_native_devices(id,user_id,platform,token_hash,encrypted_token,status,created_at,updated_at) VALUES(UNHEX(?),UNHEX(?),'android',?,X'0102','revoked',?,?),(UNHEX(?),UNHEX(?),'ios',?,X'0304','active',?,?)`, oldRevokedDevice, userID, hash("old-revoked-token"), oldNotification, oldNotification, oldActiveDevice, userID, hash("old-active-token"), oldNotification, oldNotification); err != nil {
		t.Fatal(err)
	}
	oldReminderID, activeReminderID := id(), id()
	for _, item := range []struct {
		id, status string
		at         time.Time
	}{{oldReminderID, "succeeded", oldReminder}, {activeReminderID, "scheduled", oldReminder}} {
		if _, err = server.DB.Exec(`INSERT INTO scheduled_jobs(id,owner_user_id,job_type,payload,run_at,status,created_at) VALUES(UNHEX(?),UNHEX(?),'notification.reminder','{}',? ,?,?)`, item.id, userID, time.Now().UTC().Add(time.Hour), item.status, item.at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO audit_logs(actor_user_id,action,resource_type,created_at) VALUES(UNHEX(?),'retention.old','test',?),(UNHEX(?),'retention.recent','test',?)`, userID, oldAudit, userID, recent); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO channel_accounts(id,provider,provider_user_hash,status) VALUES(UNHEX(?),'test-retention',?,'active')`, channelID, hash(channelID)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO channel_message_deliveries(channel_account_id,provider_message_id,sender_hash,user_id,status,sender_ciphertext,context_ciphertext,reply_ciphertext,created_at) VALUES(UNHEX(?),'old-message',?,UNHEX(?),'sent',X'01',X'02',X'03',?)`, channelID, hash("sender"), userID, old); err != nil {
		t.Fatal(err)
	}
	if err = server.applyRetentionPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCount := func(label, query string, args ...any) int {
		t.Helper()
		var count int
		if err = server.DB.QueryRow(query, args...).Scan(&count); err != nil {
			t.Fatalf("%s query failed: %v", label, err)
		}
		return count
	}
	if count := assertCount("expired reset", `SELECT COUNT(*) FROM password_resets WHERE id=UNHEX(?)`, resetID); count != 0 {
		t.Fatalf("expired password reset retained: %d", count)
	}
	if count := assertCount("live reset", `SELECT COUNT(*) FROM password_resets WHERE id=UNHEX(?)`, activeResetID); count != 1 {
		t.Fatalf("unexpired password reset removed: %d", count)
	}
	if count := assertCount("consumed email change", `SELECT COUNT(*) FROM email_change_requests WHERE user_id=UNHEX(?)`, userID); count != 0 {
		t.Fatalf("old email change request retained: %d", count)
	}
	if count := assertCount("old notification", `SELECT COUNT(*) FROM notifications WHERE id=UNHEX(?)`, oldNotificationID); count != 0 {
		t.Fatalf("old notification retained: %d", count)
	}
	if count := assertCount("recent notification", `SELECT COUNT(*) FROM notifications WHERE id=UNHEX(?)`, newNotificationID); count != 1 {
		t.Fatalf("recent notification removed: %d", count)
	}
	if count := assertCount("old revoked native device", `SELECT COUNT(*) FROM notification_native_devices WHERE id=UNHEX(?)`, oldRevokedDevice); count != 0 {
		t.Fatalf("old revoked native device retained: %d", count)
	}
	if count := assertCount("active native device", `SELECT COUNT(*) FROM notification_native_devices WHERE id=UNHEX(?) AND status='active'`, oldActiveDevice); count != 1 {
		t.Fatalf("active native device removed: %d", count)
	}
	if count := assertCount("old reminder", `SELECT COUNT(*) FROM scheduled_jobs WHERE id=UNHEX(?)`, oldReminderID); count != 0 {
		t.Fatalf("old completed reminder retained: %d", count)
	}
	if count := assertCount("scheduled reminder", `SELECT COUNT(*) FROM scheduled_jobs WHERE id=UNHEX(?)`, activeReminderID); count != 1 {
		t.Fatalf("scheduled reminder removed: %d", count)
	}
	if count := assertCount("expired session", `SELECT COUNT(*) FROM auth_sessions WHERE user_id=UNHEX(?)`, userID); count != 0 {
		t.Fatalf("old expired/revoked session retained: %d", count)
	}
	if count := assertCount("old audit", `SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=UNHEX(?) AND action='retention.old'`, userID); count != 0 {
		t.Fatalf("audit outside retention window retained: %d", count)
	}
	if count := assertCount("recent audit", `SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=UNHEX(?) AND action='retention.recent'`, userID); count != 1 {
		t.Fatalf("recent audit removed: %d", count)
	}
	var sender, context, reply []byte
	if err = server.DB.QueryRow(`SELECT sender_ciphertext,context_ciphertext,reply_ciphertext FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?) AND provider_message_id='old-message'`, channelID).Scan(&sender, &context, &reply); err != nil {
		t.Fatal(err)
	}
	if sender != nil || context != nil || reply != nil {
		t.Fatal("old encrypted channel payload was not purged")
	}
}
