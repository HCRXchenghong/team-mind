package live

import (
	"database/sql"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// CreateSuperAdminInvite creates a single-use code. The plaintext code is
// printed once by the bootstrap-invite command and is never stored.
func CreateSuperAdminInvite(db *sql.DB, ttl time.Duration) (string, time.Time, error) {
	raw, err := randomBytes(32)
	if err != nil {
		return "", time.Time{}, err
	}
	code := hex.EncodeToString(raw)
	expires := time.Now().UTC().Add(ttl)
	_, err = db.Exec(`INSERT INTO superadmin_invites(id,code_hash,expires_at) VALUES(UNHEX(?),?,?)`, id(), hash(code), expires)
	if err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

func (s *Server) registerSuperAdmin(w http.ResponseWriter, r *http.Request) error {
	var input struct {
		Code     string `json:"code"`
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Code) != 64 || !validEmail(input.Email) || len(input.Name) < 2 || len(input.Name) > 120 || !passwordOK(input.Password) {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请填写有效邀请码、邮箱、姓名和至少 12 位密码")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inviteID string
	var expires time.Time
	var used sql.NullTime
	err = tx.QueryRowContext(r.Context(), `SELECT HEX(id),expires_at,used_at FROM superadmin_invites WHERE code_hash=? FOR UPDATE`, hash(input.Code)).Scan(&inviteID, &expires, &used)
	if err == sql.ErrNoRows {
		return bad(http.StatusBadRequest, "INVITE_INVALID", "邀请码无效")
	}
	if err != nil {
		return err
	}
	if used.Valid {
		return bad(http.StatusConflict, "INVITE_USED", "邀请码已使用")
	}
	if time.Now().UTC().After(expires) {
		return bad(http.StatusConflict, "INVITE_EXPIRED", "邀请码已过期")
	}
	var superAdmins int
	if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM platform_roles`).Scan(&superAdmins); err != nil {
		return err
	}
	if superAdmins != 0 {
		return bad(http.StatusConflict, "SUPER_ADMIN_EXISTS", "平台超管已存在")
	}
	hashPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	uid := id()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, uid, input.Email, input.Name, hashPassword); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return bad(http.StatusConflict, "EMAIL_EXISTS", "此邮箱已注册")
		}
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO platform_roles(user_id,role) VALUES(UNHEX(?),'super_admin')`, uid); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE superadmin_invites SET used_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?)`, inviteID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusCreated, map[string]any{"registered": true, "email": input.Email})
	return nil
}
