package live

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"strings"
)

// deliverWechatNotification sends a reminder through the same OpenClaw
// send-text bridge used for chat replies. The binding keeps the latest
// inbound sender/context encrypted; per-message delivery rows are cleared
// after the reply, so they cannot be used for scheduled messages.
func (s *Server) deliverWechatNotification(ctx context.Context, job notificationDeliveryJob) (string, string, error, bool) {
	var title, body string
	err := s.DB.QueryRowContext(ctx, `SELECT n.title,n.body FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE d.id=UNHEX(?) AND d.recipient_user_id=UNHEX(?)`, job.ID, job.UserID).Scan(&title, &body)
	if err == sql.ErrNoRows {
		return "suppressed", "notification_unavailable", nil, false
	}
	if err != nil {
		return "failed", "notification_unavailable", err, false
	}
	var account, baseURL string
	var encryptedCredentials, encryptedSender, encryptedContext, scanner []byte
	err = s.DB.QueryRowContext(ctx, `SELECT HEX(c.id),c.provider_base_url,c.encrypted_credentials,c.scanner_hash,b.sender_ciphertext,b.context_ciphertext
		FROM channel_bindings b
		JOIN channel_accounts c ON c.id=b.channel_account_id
		JOIN users u ON u.id=b.user_id
		JOIN team_memberships m ON m.user_id=u.id AND m.team_id=b.current_team_id
		JOIN teams t ON t.id=m.team_id
		WHERE b.user_id=UNHEX(?) AND c.status='active' AND c.provider='openclaw-weixin' AND b.unbound_at IS NULL
		AND b.sender_ciphertext IS NOT NULL AND b.context_ciphertext IS NOT NULL
		AND u.status='active' AND m.status='active' AND t.status='active'
		ORDER BY b.bound_at DESC LIMIT 1`, job.UserID).Scan(&account, &baseURL, &encryptedCredentials, &scanner, &encryptedSender, &encryptedContext)
	if err == sql.ErrNoRows {
		return "suppressed", "wechat_contact_unavailable", nil, false
	}
	if err != nil {
		return "failed", "wechat_contact_unavailable", err, false
	}
	sender, err := decryptWechat(encryptedSender)
	if err != nil {
		return "failed", "wechat_contact_decryption_failed", err, false
	}
	if !bytes.Equal(scanner, hash(sender)) {
		return "suppressed", "wechat_contact_mismatch", nil, false
	}
	contextToken, err := decryptWechat(encryptedContext)
	if err != nil {
		return "failed", "wechat_context_decryption_failed", err, false
	}
	decryptedCredentials, err := decryptWechat(encryptedCredentials)
	if err != nil {
		return "failed", "wechat_credentials_decryption_failed", err, false
	}
	credentials, err := decodeWechatCredentials(decryptedCredentials, baseURL)
	if err != nil {
		return "failed", "wechat_credentials_invalid", err, false
	}
	gateway, err := newOpenClawWechatGateway()
	if err != nil {
		return "failed", "wechat_gateway_unavailable", err, false
	}
	text := "【TeamMind 提醒】\n" + strings.TrimSpace(title)
	if body = strings.TrimSpace(body); body != "" {
		text += "\n" + body
	}
	if err = gateway.sendText(ctx, account, credentials.BaseURL, credentials.Token, sender, contextToken, "notification-"+job.ID, truncateRunes(text, 1800)); err != nil {
		log.Printf("wechat reminder delivery failed for user %s", job.UserID)
		return "failed", "wechat_send_failed", err, false
	}
	return "delivered", "", nil, false
}

// sendWechatProactive sends a text message using the most recent inbound
// contact/context kept on the binding. It is used by the explicit
// "send test message" action and keeps the same safety checks as notification
// delivery: active account, active membership, sender hash match and decryptable
// credentials. A non-empty reason means the message was not attempted.
func (s *Server) sendWechatProactive(ctx context.Context, userID, clientID, text string) (string, error) {
	var account, baseURL string
	var encryptedCredentials, encryptedSender, encryptedContext, scanner []byte
	err := s.DB.QueryRowContext(ctx, `SELECT HEX(c.id),c.provider_base_url,c.encrypted_credentials,c.scanner_hash,b.sender_ciphertext,b.context_ciphertext
		FROM channel_bindings b
		JOIN channel_accounts c ON c.id=b.channel_account_id
		JOIN users u ON u.id=b.user_id
		JOIN team_memberships m ON m.user_id=u.id AND m.team_id=b.current_team_id
		JOIN teams t ON t.id=m.team_id
		WHERE b.user_id=UNHEX(?) AND c.status='active' AND c.provider='openclaw-weixin' AND b.unbound_at IS NULL
		AND b.sender_ciphertext IS NOT NULL AND b.context_ciphertext IS NOT NULL
		AND u.status='active' AND m.status='active' AND t.status='active'
		ORDER BY b.bound_at DESC LIMIT 1`, userID).Scan(&account, &baseURL, &encryptedCredentials, &scanner, &encryptedSender, &encryptedContext)
	if err == sql.ErrNoRows {
		return "wechat_contact_unavailable", nil
	}
	if err != nil {
		return "wechat_contact_unavailable", err
	}
	sender, err := decryptWechat(encryptedSender)
	if err != nil {
		return "wechat_contact_decryption_failed", err
	}
	if !bytes.Equal(scanner, hash(sender)) {
		return "wechat_contact_mismatch", nil
	}
	contextToken, err := decryptWechat(encryptedContext)
	if err != nil {
		return "wechat_context_decryption_failed", err
	}
	decryptedCredentials, err := decryptWechat(encryptedCredentials)
	if err != nil {
		return "wechat_credentials_decryption_failed", err
	}
	credentials, err := decodeWechatCredentials(decryptedCredentials, baseURL)
	if err != nil {
		return "wechat_credentials_invalid", err
	}
	gateway, err := newOpenClawWechatGateway()
	if err != nil {
		return "wechat_gateway_unavailable", err
	}
	if err = gateway.sendText(ctx, account, credentials.BaseURL, credentials.Token, sender, contextToken, clientID, truncateRunes(text, 1800)); err != nil {
		return "wechat_send_failed", err
	}
	return "", nil
}
