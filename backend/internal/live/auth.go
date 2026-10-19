package live

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func passwordOK(p string) bool { return len(p) >= 12 && len(p) <= 72 }

func validEmail(email string) bool {
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	parsed, err := mail.ParseAddress(email)
	return err == nil && parsed.Address == email && strings.Count(email, "@") == 1
}

func (s *Server) auth(w http.ResponseWriter, r *http.Request, path string) error {
	switch path {
	case "register":
		if e := method(r, "POST"); e != nil {
			return e
		}
		var x struct {
			Email    string `json:"email"`
			Name     string `json:"name"`
			Password string `json:"password"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		x.Email = strings.ToLower(strings.TrimSpace(x.Email))
		x.Name = strings.TrimSpace(x.Name)
		if !validEmail(x.Email) || len(x.Name) < 2 || len(x.Name) > 120 || !passwordOK(x.Password) {
			return bad(400, "VALIDATION_ERROR", "请填写有效邮箱、姓名和至少 12 位密码")
		}
		h, e := bcrypt.GenerateFromPassword([]byte(x.Password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		uid := id()
		_, e = s.DB.ExecContext(r.Context(), `INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, uid, x.Email, x.Name, h)
		if e != nil {
			if strings.Contains(e.Error(), "Duplicate") {
				return bad(409, "EMAIL_EXISTS", "此邮箱已注册")
			}
			return e
		}
		write(w, 201, map[string]string{"id": uid})
		return nil
	case "login":
		if e := method(r, "POST"); e != nil {
			return e
		}
		var x struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Remember bool   `json:"remember"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		x.Email = strings.ToLower(strings.TrimSpace(x.Email))
		if x.Email == "" || x.Password == "" {
			return bad(400, "VALIDATION_ERROR", "请输入邮箱和密码")
		}
		var locked sql.NullTime
		_ = s.DB.QueryRowContext(r.Context(), `SELECT locked_until FROM login_failures WHERE email=?`, x.Email).Scan(&locked)
		if locked.Valid && locked.Time.After(time.Now().UTC()) {
			return bad(429, "LOGIN_LOCKED", "登录失败次数过多，请稍后再试")
		}
		var uid, pass, status string
		e := s.DB.QueryRowContext(r.Context(), `SELECT HEX(id),password_hash,status FROM users WHERE email=? AND deleted_at IS NULL`, x.Email).Scan(&uid, &pass, &status)
		if e != nil || status != "active" || bcrypt.CompareHashAndPassword([]byte(pass), []byte(x.Password)) != nil {
			_, _ = s.DB.ExecContext(r.Context(), `INSERT INTO login_failures(email,failures,locked_until) VALUES(?,1,NULL) ON DUPLICATE KEY UPDATE failures=IF(locked_until<UTC_TIMESTAMP(3),1,failures+1),locked_until=IF(failures>=5,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 15 MINUTE),NULL)`, x.Email)
			return bad(401, "INVALID_CREDENTIALS", "邮箱或密码错误")
		}
		_, _ = s.DB.ExecContext(r.Context(), `DELETE FROM login_failures WHERE email=?`, x.Email)
		var mfaEnabled bool
		if e = s.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_mfa WHERE user_id=UNHEX(?) AND enabled_at IS NOT NULL)`, uid).Scan(&mfaEnabled); e != nil {
			return e
		}
		if mfaEnabled {
			challengeToken := token()
			tx, beginErr := s.DB.BeginTx(r.Context(), nil)
			if beginErr != nil {
				return beginErr
			}
			defer tx.Rollback()
			if _, e = tx.ExecContext(r.Context(), `DELETE FROM auth_mfa_challenges WHERE expires_at<=UTC_TIMESTAMP(3) OR consumed_at IS NOT NULL`); e != nil {
				return e
			}
			if _, e = tx.ExecContext(r.Context(), `INSERT INTO auth_mfa_challenges(id,user_id,token_hash,remember_login,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 5 MINUTE))`, id(), uid, hash(challengeToken), x.Remember); e != nil {
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			write(w, 200, map[string]any{"mfaRequired": true, "challengeToken": challengeToken, "expiresIn": 300})
			return nil
		}
		t := token()
		duration := 8 * time.Hour
		if x.Remember {
			duration = 30 * 24 * time.Hour
		}
		_, e = s.DB.ExecContext(r.Context(), `INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), uid, hash(t), time.Now().UTC().Add(duration))
		if e != nil {
			return e
		}
		write(w, 200, map[string]any{"accessToken": t, "expiresIn": int(duration.Seconds())})
		return nil
	case "logout":
		if e := method(r, "POST"); e != nil {
			return e
		}
		a := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if a != "" {
			_, _ = s.DB.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=UTC_TIMESTAMP(3) WHERE token_hash=?`, hash(a))
		}
		write(w, 200, map[string]bool{"revoked": true})
		return nil
	case "refresh":
		if e := method(r, "POST"); e != nil {
			return e
		}
		u, e := s.currentUser(r)
		if e != nil {
			return e
		}
		old := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		newToken := token()
		newID := id()
		expires := time.Now().UTC().Add(8 * time.Hour)
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		res, e := tx.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=UTC_TIMESTAMP(3) WHERE token_hash=? AND revoked_at IS NULL AND expires_at>UTC_TIMESTAMP(3)`, hash(old))
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(401, "SESSION_EXPIRED", "会话已过期")
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, newID, u.ID, hash(newToken), expires)
		if e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]any{"accessToken": newToken, "expiresIn": 28800})
		return nil
	case "forgot":
		if e := method(r, "POST"); e != nil {
			return e
		}
		var x struct {
			Email string `json:"email"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		x.Email = strings.ToLower(strings.TrimSpace(x.Email))
		var uid string
		e := s.DB.QueryRowContext(r.Context(), `SELECT HEX(id) FROM users WHERE email=? AND status='active'`, x.Email).Scan(&uid)
		if e == nil {
			t := token()
			_, e = s.DB.ExecContext(r.Context(), `INSERT INTO password_resets(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 20 MINUTE))`, id(), uid, hash(t))
			if e != nil {
				return e
			}
			if s.development {
				write(w, 200, map[string]string{"message": "开发模式：重置令牌仅在此响应显示", "resetToken": t})
				return nil
			}
			if e = sendResetMail(x.Email, t); e != nil {
				return e
			}
		}
		write(w, 200, map[string]string{"message": "如果账号存在，重置说明将发送到该邮箱"})
		return nil
	case "reset":
		if e := method(r, "POST"); e != nil {
			return e
		}
		var x struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if !passwordOK(x.Password) || len(x.Token) != 64 {
			return bad(400, "VALIDATION_ERROR", "密码至少 12 位，且重置链接必须有效")
		}
		h, e := bcrypt.GenerateFromPassword([]byte(x.Password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var uid string
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(user_id) FROM password_resets WHERE token_hash=? AND used_at IS NULL AND expires_at>UTC_TIMESTAMP(3) FOR UPDATE`, hash(x.Token)).Scan(&uid)
		if e == sql.ErrNoRows {
			return bad(400, "RESET_EXPIRED", "重置链接无效或已过期")
		}
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE users SET password_hash=? WHERE id=UNHEX(?)`, h, uid); e != nil {
			return e
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE password_resets SET used_at=UTC_TIMESTAMP(3) WHERE token_hash=?`, hash(x.Token)); e != nil {
			return e
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND revoked_at IS NULL`, uid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"changed": true})
		return nil
	case "password":
		if e := method(r, "POST"); e != nil {
			return e
		}
		u, e := s.currentUser(r)
		if e != nil {
			return e
		}
		var x struct {
			Current  string `json:"current"`
			Password string `json:"password"`
		}
		if e = body(w, r, &x); e != nil {
			return e
		}
		if !passwordOK(x.Password) {
			return bad(400, "VALIDATION_ERROR", "新密码至少 12 位")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var old string
		if e = tx.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&old); e != nil {
			return e
		}
		if bcrypt.CompareHashAndPassword([]byte(old), []byte(x.Current)) != nil {
			return bad(401, "INVALID_CREDENTIALS", "当前密码错误")
		}
		h, e := bcrypt.GenerateFromPassword([]byte(x.Password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE users SET password_hash=? WHERE id=UNHEX(?)`, h, u.ID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND revoked_at IS NULL`, u.ID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"changed": true})
		return nil
	case "sessions":
		if e := method(r, "GET"); e != nil {
			return e
		}
		u, e := s.currentUser(r)
		if e != nil {
			return e
		}
		currentTokenHash := hash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(id),expires_at,created_at,token_hash=? FROM auth_sessions WHERE user_id=UNHEX(?) AND revoked_at IS NULL AND expires_at>UTC_TIMESTAMP(3) ORDER BY created_at DESC`, currentTokenHash, u.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var sid string
			var expiry, created time.Time
			var current bool
			if e = rows.Scan(&sid, &expiry, &created, &current); e != nil {
				return e
			}
			out = append(out, map[string]any{"id": sid, "expiresAt": expiry, "createdAt": created, "current": current})
		}
		write(w, 200, out)
		return rows.Err()
	default:
		if strings.HasPrefix(path, "sessions/") && r.Method == "DELETE" {
			u, e := s.currentUser(r)
			if e != nil {
				return e
			}
			sid := strings.TrimPrefix(path, "sessions/")
			if !validID(sid) {
				return bad(400, "INVALID_ID", "会话 ID 无效")
			}
			res, e := s.DB.ExecContext(r.Context(), `UPDATE auth_sessions SET revoked_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND user_id=UNHEX(?) AND revoked_at IS NULL`, sid, u.ID)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return bad(404, "NOT_FOUND", "会话不存在")
			}
			write(w, 200, map[string]bool{"revoked": true})
			return nil
		}
		return bad(404, "NOT_FOUND", "资源不存在")
	}
}
func sendResetMail(to, t string) error {
	host := os.Getenv("SMTP_HOST")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")
	base := os.Getenv("PUBLIC_URL")
	if host == "" || user == "" || pass == "" || from == "" || base == "" {
		return bad(503, "MAIL_NOT_CONFIGURED", "邮件服务未配置，请联系管理员")
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "465"
	}
	c, e := secureSMTPClient(host, port)
	if e != nil {
		return bad(503, "MAIL_UNAVAILABLE", "邮件服务暂时不可用")
	}
	defer c.Close()
	if e = c.Auth(smtp.PlainAuth("", user, pass, host)); e != nil {
		return e
	}
	if e = c.Mail(from); e != nil {
		return e
	}
	if e = c.Rcpt(to); e != nil {
		return e
	}
	wc, e := c.Data()
	if e != nil {
		return e
	}
	link := strings.TrimSuffix(base, "/") + "/reset-password?token=" + t
	_, e = fmt.Fprintf(wc, "From: %s\r\nTo: %s\r\nSubject: TeamMind password reset\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nOpen this link within 20 minutes: %s\r\n", from, to, link)
	if e != nil {
		return e
	}
	return wc.Close()
}

func secureSMTPClient(host, port string) (*smtp.Client, error) {
	var conn net.Conn
	var err error
	if port == "465" {
		conn, err = tls.Dial("tcp", net.JoinHostPort(host, port), &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
	}
	if err != nil {
		return nil, err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if port != "465" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			client.Close()
			return nil, fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			client.Close()
			return nil, err
		}
	}
	return client, nil
}

func BootstrapAdmin(db *sql.DB, email, name, password string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) || len(strings.TrimSpace(name)) < 2 || len(strings.TrimSpace(name)) > 120 || !passwordOK(password) {
		return fmt.Errorf("valid email and password of at least 12 characters required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, e := db.Conn(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	var locked int
	if e = conn.QueryRowContext(ctx, `SELECT GET_LOCK('teammind.bootstrap_admin',10)`).Scan(&locked); e != nil {
		return e
	}
	if locked != 1 {
		return fmt.Errorf("could not acquire super admin bootstrap lock")
	}
	defer conn.ExecContext(context.Background(), `SELECT RELEASE_LOCK('teammind.bootstrap_admin')`)
	tx, e := conn.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM platform_roles`).Scan(&count); e != nil {
		return e
	}
	if count != 0 {
		return fmt.Errorf("super admin already exists")
	}
	uid := id()
	h, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, uid, email, name, h)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO platform_roles(user_id,role) VALUES(UNHEX(?),'super_admin')`, uid)
	if e != nil {
		return e
	}
	return tx.Commit()
}
