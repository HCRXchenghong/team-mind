package live

import (
	"context"
	"log"
	"time"
)

// Data-retention windows are centralized here and described in README. Shared
// team records are not pruned by this worker; only expired credentials,
// delivery history, reminders, and old audit records are eligible.
const (
	retentionBatchSize       = 1000
	retentionMaxBatches      = 20
	authArtifactRetention    = 30 * 24 * time.Hour
	sessionHistoryRetention  = 90 * 24 * time.Hour
	invitationRetention      = 90 * 24 * time.Hour
	reminderHistoryRetention = 90 * 24 * time.Hour
	channelPayloadRetention  = 30 * 24 * time.Hour
	channelDedupeRetention   = 180 * 24 * time.Hour
	nativeDeviceRetention    = 365 * 24 * time.Hour
	notificationRetention    = 365 * 24 * time.Hour
	auditLogRetention        = 2555 * 24 * time.Hour
)

type retentionRule struct {
	name       string
	query      string
	cutoff     time.Time
	twoCutoffs bool
}

// RunRetentionWorker applies bounded deletes hourly. Each row set is removed
// in small autocommit batches so it can share the database with live traffic.
func (s *Server) RunRetentionWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	lastErrorLog := time.Time{}
	for {
		if err := s.applyRetentionPolicy(ctx); err != nil && ctx.Err() == nil && time.Since(lastErrorLog) >= 30*time.Second {
			log.Printf("data retention worker failed; will retry")
			lastErrorLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) applyRetentionPolicy(ctx context.Context) error {
	now := time.Now().UTC()
	rules := []retentionRule{
		{"expired password reset tokens", `DELETE FROM password_resets WHERE created_at<? AND (used_at IS NOT NULL OR expires_at<?) LIMIT 1000`, now.Add(-authArtifactRetention), true},
		{"expired email change tokens", `DELETE FROM email_change_requests WHERE created_at<? AND (consumed_at IS NOT NULL OR expires_at<?) LIMIT 1000`, now.Add(-authArtifactRetention), true},
		{"expired MFA challenges", `DELETE FROM auth_mfa_challenges WHERE created_at<? AND (consumed_at IS NOT NULL OR expires_at<?) LIMIT 1000`, now.Add(-authArtifactRetention), true},
		{"expired and revoked sessions", `DELETE FROM auth_sessions WHERE created_at<? AND (revoked_at IS NOT NULL OR expires_at<?) LIMIT 1000`, now.Add(-sessionHistoryRetention), true},
		{"stale login failures", `DELETE FROM login_failures WHERE updated_at<? AND (locked_until IS NULL OR locked_until<?) LIMIT 1000`, now.Add(-authArtifactRetention), true},
		{"stale IP rate limits", `DELETE FROM auth_ip_rate_limits WHERE updated_at<? LIMIT 1000`, now.Add(-authArtifactRetention), false},
		{"expired invitations", `DELETE FROM team_invitations WHERE created_at<? AND (revoked_at IS NOT NULL OR expires_at<?) LIMIT 1000`, now.Add(-invitationRetention), true},
		{"completed reminder history", `DELETE FROM scheduled_jobs WHERE job_type='notification.reminder' AND created_at<? AND status IN ('succeeded','failed','cancelled') LIMIT 1000`, now.Add(-reminderHistoryRetention), false},
		{"old channel message payloads", `UPDATE channel_message_deliveries SET sender_ciphertext=NULL,context_ciphertext=NULL,reply_ciphertext=NULL WHERE created_at<? AND status IN ('sent','rejected','failed') AND (sender_ciphertext IS NOT NULL OR context_ciphertext IS NOT NULL OR reply_ciphertext IS NOT NULL) LIMIT 1000`, now.Add(-channelPayloadRetention), false},
		{"old channel message dedupe records", `DELETE FROM channel_message_deliveries WHERE created_at<? AND status IN ('sent','rejected','failed') LIMIT 1000`, now.Add(-channelDedupeRetention), false},
		{"old notification deliveries", `DELETE FROM notification_deliveries WHERE created_at<? AND status IN ('delivered','suppressed','failed') LIMIT 1000`, now.Add(-notificationRetention), false},
		{"old revoked native devices", `DELETE FROM notification_native_devices WHERE status='revoked' AND updated_at<? AND NOT EXISTS (SELECT 1 FROM notification_deliveries d WHERE d.native_device_id=notification_native_devices.id) LIMIT 1000`, now.Add(-nativeDeviceRetention), false},
		{"old unreferenced notifications", `DELETE FROM notifications WHERE created_at<? AND NOT EXISTS (SELECT 1 FROM notification_deliveries d WHERE d.notification_id=notifications.id) LIMIT 1000`, now.Add(-notificationRetention), false},
		{"old audit records", `DELETE FROM audit_logs WHERE created_at<? LIMIT 1000`, now.Add(-auditLogRetention), false},
	}
	for _, rule := range rules {
		for batch := 0; batch < retentionMaxBatches; batch++ {
			args := []any{rule.cutoff}
			if rule.twoCutoffs {
				args = append(args, rule.cutoff)
			}
			result, err := s.DB.ExecContext(ctx, rule.query, args...)
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if changed == 0 || changed < retentionBatchSize {
				break
			}
		}
	}
	return nil
}
