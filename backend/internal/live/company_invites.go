package live

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// companyInvites lets a platform super admin issue links that create a new
// company (or single-layer team) together with its first owner account. It is
// separate from team invitations, which add employees to an existing team.
func (s *Server) companyInvites(w http.ResponseWriter, r *http.Request, u User, path string) error {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	// Accepted paths: platform/company-invites and platform/company-invites/{id}.
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			return s.listCompanyInvites(w, r)
		case http.MethodPost:
			return s.createCompanyInvite(w, r, u)
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持的请求方法")
		}
	}
	if len(parts) == 3 {
		if r.Method != http.MethodDelete {
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持的请求方法")
		}
		return s.revokeCompanyInvite(w, r, parts[2])
	}
	return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
}

func (s *Server) listCompanyInvites(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(i.id),i.expires_at,i.max_uses,i.used_count,i.note,i.revoked_at,i.created_at,COALESCE(u.display_name,'')
		FROM company_invites i LEFT JOIN users u ON u.id=i.created_by
		ORDER BY i.created_at DESC LIMIT 100`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := make([]map[string]any, 0)
	now := time.Now().UTC()
	for rows.Next() {
		var id, note, creator string
		var expires, created time.Time
		var maxUses, used int
		var revoked sql.NullTime
		if err = rows.Scan(&id, &expires, &maxUses, &used, &note, &revoked, &created, &creator); err != nil {
			return err
		}
		status := "active"
		if revoked.Valid {
			status = "revoked"
		} else if now.After(expires) {
			status = "expired"
		} else if used >= maxUses {
			status = "used"
		}
		out = append(out, map[string]any{
			"id": id, "expiresAt": expires, "maxUses": maxUses, "usedCount": used,
			"note": note, "status": status, "createdAt": created, "createdBy": creator,
		})
	}
	write(w, http.StatusOK, out)
	return rows.Err()
}

func (s *Server) createCompanyInvite(w http.ResponseWriter, r *http.Request, u User) error {
	var input struct {
		ExpiresInHours int    `json:"expiresInHours"`
		MaxUses        int    `json:"maxUses"`
		Note           string `json:"note"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	if input.ExpiresInHours == 0 {
		input.ExpiresInHours = 168
	}
	if input.MaxUses == 0 {
		input.MaxUses = 1
	}
	input.Note = strings.TrimSpace(input.Note)
	if input.ExpiresInHours < 1 || input.ExpiresInHours > 720 || input.MaxUses < 1 || input.MaxUses > 100 || len([]rune(input.Note)) > 160 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "有效期需在 1–720 小时，次数需在 1–100 次，备注不超过 160 字")
	}
	code := token()
	expires := time.Now().UTC().Add(time.Duration(input.ExpiresInHours) * time.Hour)
	inviteID := id()
	if _, err := s.DB.ExecContext(r.Context(), `INSERT INTO company_invites(id,code_hash,created_by,expires_at,max_uses,note) VALUES(UNHEX(?),?,UNHEX(?),?,?,?)`, inviteID, hash(code), u.ID, expires, input.MaxUses, input.Note); err != nil {
		return err
	}
	write(w, http.StatusCreated, map[string]any{
		"id": inviteID, "code": code, "path": "/company-invite?code=" + code,
		"expiresAt": expires, "maxUses": input.MaxUses, "note": input.Note,
	})
	return nil
}

func (s *Server) revokeCompanyInvite(w http.ResponseWriter, r *http.Request, inviteID string) error {
	if !validID(inviteID) {
		return bad(http.StatusBadRequest, "INVALID_ID", "邀请 ID 无效")
	}
	result, err := s.DB.ExecContext(r.Context(), `UPDATE company_invites SET revoked_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND revoked_at IS NULL`, inviteID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return bad(http.StatusNotFound, "NOT_FOUND", "邀请不存在或已撤销")
	}
	write(w, http.StatusOK, map[string]bool{"revoked": true})
	return nil
}

func (s *Server) companyInviteStatus(w http.ResponseWriter, r *http.Request) error {
	if err := method(r, "GET"); err != nil {
		return err
	}
	code := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("code")))
	if len(code) != 64 {
		write(w, http.StatusOK, map[string]any{"valid": false, "reason": "invalid"})
		return nil
	}
	var expires time.Time
	var maxUses, used int
	var revoked sql.NullTime
	err := s.DB.QueryRowContext(r.Context(), `SELECT expires_at,max_uses,used_count,revoked_at FROM company_invites WHERE code_hash=?`, hash(code)).Scan(&expires, &maxUses, &used, &revoked)
	if err == sql.ErrNoRows {
		write(w, http.StatusOK, map[string]any{"valid": false, "reason": "invalid"})
		return nil
	}
	if err != nil {
		return err
	}
	reason := ""
	switch {
	case revoked.Valid:
		reason = "revoked"
	case time.Now().UTC().After(expires):
		reason = "expired"
	case used >= maxUses:
		reason = "used"
	}
	remaining := maxUses - used
	if remaining < 0 {
		remaining = 0
	}
	write(w, http.StatusOK, map[string]any{
		"valid": reason == "", "reason": reason, "expiresAt": expires,
		"remainingUses": remaining,
	})
	return nil
}

// registerCompany creates the first owner account and company for an invite.
// The invite is issued by the platform super admin, so the new company is
// active immediately even when TEAM_APPROVAL_REQUIRED=1.
func (s *Server) registerCompany(w http.ResponseWriter, r *http.Request) error {
	if err := method(r, "POST"); err != nil {
		return err
	}
	var input struct {
		Code        string `json:"code"`
		CompanyName string `json:"companyName"`
		CompanyKind string `json:"companyKind"`
		Name        string `json:"name"`
		Email       string `json:"email"`
		Password    string `json:"password"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.CompanyName = strings.TrimSpace(input.CompanyName)
	input.CompanyKind = strings.TrimSpace(input.CompanyKind)
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if input.CompanyKind == "" {
		input.CompanyKind = "company"
	}
	if len(input.Code) != 64 || len([]rune(input.CompanyName)) < 2 || len([]rune(input.CompanyName)) > 160 ||
		(input.CompanyKind != "company" && input.CompanyKind != "team") ||
		len([]rune(input.Name)) < 2 || len([]rune(input.Name)) > 120 ||
		!validEmail(input.Email) || !passwordOK(input.Password) {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请填写有效的公司邀请码、公司名称、姓名、邮箱和至少 12 位密码")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inviteID string
	var expires time.Time
	var maxUses, used int
	var revoked sql.NullTime
	err = tx.QueryRowContext(r.Context(), `SELECT HEX(id),expires_at,max_uses,used_count,revoked_at FROM company_invites WHERE code_hash=? FOR UPDATE`, hash(input.Code)).Scan(&inviteID, &expires, &maxUses, &used, &revoked)
	if err == sql.ErrNoRows {
		return bad(http.StatusNotFound, "COMPANY_INVITE_INVALID", "公司邀请链接无效")
	}
	if err != nil {
		return err
	}
	if revoked.Valid || time.Now().UTC().After(expires) || used >= maxUses {
		return bad(http.StatusGone, "COMPANY_INVITE_EXPIRED", "公司邀请已过期、撤销或用尽")
	}
	var userID, existingHash, existingStatus string
	err = tx.QueryRowContext(r.Context(), `SELECT HEX(id),password_hash,status FROM users WHERE email=? AND deleted_at IS NULL FOR UPDATE`, input.Email).Scan(&userID, &existingHash, &existingStatus)
	if err == nil {
		// An existing account can use a company invite too, but must prove the
		// password. This avoids taking over an account by possessing a link.
		if existingStatus != "active" {
			return bad(http.StatusForbidden, "ACCOUNT_DISABLED", "该账号不可用")
		}
		if bcrypt.CompareHashAndPassword([]byte(existingHash), []byte(input.Password)) != nil {
			return bad(http.StatusUnauthorized, "INVALID_CREDENTIALS", "该邮箱已注册，请填写正确的原密码，或更换邮箱")
		}
	} else if err == sql.ErrNoRows {
		hashed, hashErr := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return hashErr
		}
		userID = id()
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, input.Email, input.Name, hashed); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return bad(http.StatusConflict, "EMAIL_EXISTS", "此邮箱已注册")
			}
			return err
		}
	} else {
		return err
	}
	teamID := id()
	settings := []byte(`{"description":"","timezone":"Asia/Shanghai"}`)
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO teams(id,name,kind,slug,status,created_by,settings) VALUES(UNHEX(?),?,?,?,'active',UNHEX(?),?)`, teamID, input.CompanyName, input.CompanyKind, "team-"+teamID, userID, settings); err != nil {
		return err
	}
	if input.CompanyKind == "company" {
		departmentID := id()
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO departments(id,team_id,name) VALUES(UNHEX(?),UNHEX(?),'默认部门')`, departmentID, teamID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO team_memberships(team_id,user_id,role,status,department_id,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UNHEX(?),UTC_TIMESTAMP(3))`, teamID, userID, departmentID); err != nil {
			return err
		}
	} else if _, err = tx.ExecContext(r.Context(), `INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UTC_TIMESTAMP(3))`, teamID, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE company_invites SET used_count=used_count+1 WHERE id=UNHEX(?)`, inviteID); err != nil {
		return err
	}
	if err = addAudit(tx, userID, teamID, "company.invite.register", "company_invite", inviteID); err != nil {
		return err
	}
	accessToken := token()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), userID, hash(accessToken), time.Now().UTC().Add(8*time.Hour)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusCreated, map[string]any{"accessToken": accessToken, "teamId": teamID})
	return nil
}
