package live

import (
	"database/sql"
	"fmt"
	"mime"
	"net/http"
	"net/smtp"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func (s *Server) requestEmailChange(w http.ResponseWriter, r *http.Request, u User) error {
	if err := method(r, http.MethodPost); err != nil {
		return err
	}
	if err := s.enforceRequestRateLimit(r, "email-change-request"); err != nil {
		return err
	}
	var input struct {
		Email           string `json:"email"`
		CurrentPassword string `json:"currentPassword"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if !validEmail(input.Email) || input.CurrentPassword == "" || len(input.CurrentPassword) > 72 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请填写有效的新邮箱并验证当前密码")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldEmail, passwordHash, status string
	if err = tx.QueryRowContext(r.Context(), `SELECT email,password_hash,status FROM users WHERE id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&oldEmail, &passwordHash, &status); err != nil {
		return err
	}
	if status != "active" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.CurrentPassword)) != nil {
		return bad(http.StatusUnauthorized, "INVALID_CREDENTIALS", "当前密码错误")
	}
	if strings.EqualFold(oldEmail, input.Email) {
		return bad(http.StatusConflict, "EMAIL_UNCHANGED", "新邮箱与当前邮箱相同")
	}
	var exists int
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE email=? AND id<>UNHEX(?))`, input.Email, u.ID).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 {
		return bad(http.StatusConflict, "EMAIL_EXISTS", "此邮箱已被其他账号使用")
	}
	verificationToken := token()
	if _, err = tx.ExecContext(r.Context(), `UPDATE email_change_requests SET consumed_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND consumed_at IS NULL`, u.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO email_change_requests(id,user_id,new_email,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 30 MINUTE))`, id(), u.ID, input.Email, hash(verificationToken)); err != nil {
		return err
	}
	// Keep email addresses out of the security audit while recording that the
	// owner initiated a change request.
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),'user.email_change_requested','user',UNHEX(?),JSON_OBJECT('newEmail',?))`, u.ID, u.ID, maskedEmail(input.Email)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if s.development {
		write(w, http.StatusOK, map[string]any{"message": "开发模式：验证令牌仅用于本地验收", "verificationToken": verificationToken})
		return nil
	}
	if err = sendEmailChangeMail(input.Email, verificationToken); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]string{"message": "验证邮件已发送到新邮箱；验证前账号邮箱不会改变"})
	return nil
}

func (s *Server) confirmEmailChange(w http.ResponseWriter, r *http.Request) error {
	if err := method(r, http.MethodPost); err != nil {
		return err
	}
	if err := s.enforceRequestRateLimit(r, "email-change-confirm"); err != nil {
		return err
	}
	var input struct {
		Token string `json:"token"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	if len(input.Token) != 64 {
		return bad(http.StatusBadRequest, "EMAIL_CHANGE_INVALID", "邮箱验证链接无效或已过期")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The high-entropy, short-lived, single-use token proves control of the new
	// mailbox. The old browser session is intentionally not required, so a user
	// can open the link on another device or after the old session expires.
	var userID string
	if err = tx.QueryRowContext(r.Context(), `SELECT LOWER(HEX(user_id)) FROM email_change_requests WHERE token_hash=? AND consumed_at IS NULL AND expires_at>UTC_TIMESTAMP(3)`, hash(input.Token)).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return bad(http.StatusBadRequest, "EMAIL_CHANGE_INVALID", "邮箱验证链接无效或已过期")
		}
		return err
	}
	var oldEmail, status string
	if err = tx.QueryRowContext(r.Context(), `SELECT email,status FROM users WHERE id=UNHEX(?) FOR UPDATE`, userID).Scan(&oldEmail, &status); err != nil {
		return err
	}
	if status != "active" {
		return bad(http.StatusConflict, "ACCOUNT_INACTIVE", "账号当前不可修改邮箱")
	}
	var newEmail string
	if err = tx.QueryRowContext(r.Context(), `SELECT new_email FROM email_change_requests WHERE token_hash=? AND user_id=UNHEX(?) AND consumed_at IS NULL AND expires_at>UTC_TIMESTAMP(3) FOR UPDATE`, hash(input.Token), userID).Scan(&newEmail); err != nil {
		if err == sql.ErrNoRows {
			return bad(http.StatusBadRequest, "EMAIL_CHANGE_INVALID", "邮箱验证链接无效或已过期")
		}
		return err
	}
	var occupied int
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE email=? AND id<>UNHEX(?))`, newEmail, userID).Scan(&occupied); err != nil {
		return err
	}
	if occupied == 1 {
		return bad(http.StatusConflict, "EMAIL_EXISTS", "此邮箱已被其他账号使用，请重新发起邮箱变更")
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET email=? WHERE id=UNHEX(?)`, newEmail, userID); err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			return bad(http.StatusConflict, "EMAIL_EXISTS", "此邮箱已被其他账号使用，请重新发起邮箱变更")
		}
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE email_change_requests SET consumed_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND consumed_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE password_resets SET used_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND used_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,before_summary,after_summary) VALUES(UNHEX(?),'user.email_changed','user',UNHEX(?),JSON_OBJECT('email',?),JSON_OBJECT('email',?))`, userID, userID, maskedEmail(oldEmail), maskedEmail(newEmail)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]any{"changed": true, "sessionsRevoked": true, "email": newEmail})
	return nil
}

func maskedEmail(address string) string {
	parts := strings.SplitN(address, "@", 2)
	if len(parts) != 2 {
		return "***"
	}
	local := []rune(parts[0])
	if len(local) > 2 {
		local = local[:2]
	} else if len(local) == 2 {
		local = local[:1]
	}
	return string(local) + "***@" + parts[1]
}

func sendEmailChangeMail(to, verificationToken string) error {
	// Reuse the production SMTP configuration used for password recovery.
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")
	base := os.Getenv("PUBLIC_URL")
	if host == "" || user == "" || pass == "" || from == "" || base == "" {
		return bad(http.StatusServiceUnavailable, "MAIL_NOT_CONFIGURED", "邮件服务未配置，请联系管理员")
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "465"
	}
	client, err := secureSMTPClient(host, port)
	if err != nil {
		return bad(http.StatusServiceUnavailable, "MAIL_UNAVAILABLE", "邮件服务暂时不可用")
	}
	defer client.Close()
	if err = client.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
		return bad(http.StatusServiceUnavailable, "MAIL_UNAVAILABLE", "邮件服务暂时不可用")
	}
	if err = client.Mail(from); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	link := strings.TrimSuffix(base, "/") + "/verify-email-change#verifyEmailToken=" + verificationToken
	if _, err = fmt.Fprintf(writer, "From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n请在 30 分钟内打开以下链接，确认 TeamMind 新邮箱地址：%s\r\n如果这不是你本人发起的操作，请忽略此邮件。\r\n", from, to, mime.QEncoding.Encode("UTF-8", "TeamMind 邮箱变更验证"), link); err != nil {
		return err
	}
	return writer.Close()
}
