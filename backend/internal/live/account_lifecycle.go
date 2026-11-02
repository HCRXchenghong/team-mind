package live

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// deleteAccount erases account-owned content and credentials while retaining
// team business records and security audit history under an anonymized user
// tombstone. The transaction refuses deletion until team ownership is handed
// over and protects the last active platform administrator.
func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request, u User) error {
	if err := method(r, http.MethodPost); err != nil {
		return err
	}
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		MFAcode         string `json:"mfaCode"`
		Confirmation    string `json:"confirmation"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.MFAcode = strings.TrimSpace(input.MFAcode)
	if len(input.CurrentPassword) == 0 || len(input.CurrentPassword) > 72 || input.Confirmation != "删除账号" || len(input.MFAcode) > 64 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请输入当前密码、动态验证码（如已启用）并准确填写“删除账号”")
	}
	deletionPassword, err := bcrypt.GenerateFromPassword([]byte(token()), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var passwordHash, status, email string
	if err = tx.QueryRowContext(r.Context(), `SELECT password_hash,status,email FROM users WHERE id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&passwordHash, &status, &email); err != nil {
		return err
	}
	if status != "active" || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.CurrentPassword)) != nil {
		return bad(http.StatusUnauthorized, "CURRENT_PASSWORD_INVALID", "当前密码错误或账号不可用")
	}
	var encrypted []byte
	var enabled sql.NullTime
	var lastStep sql.NullInt64
	mfaErr := tx.QueryRowContext(r.Context(), `SELECT secret_ciphertext,enabled_at,last_totp_step FROM user_mfa WHERE user_id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&encrypted, &enabled, &lastStep)
	if mfaErr != nil && mfaErr != sql.ErrNoRows {
		return mfaErr
	}
	if mfaErr == nil && enabled.Valid {
		secret, decryptErr := decryptMFASecret(u.ID, encrypted)
		if decryptErr != nil {
			return decryptErr
		}
		acceptedStep, valid := matchingTOTPStep(secret, input.MFAcode, time.Now().UTC())
		if valid && lastStep.Valid && acceptedStep <= lastStep.Int64 {
			valid = false
		}
		usedTOTP := valid
		if !valid && verifyRecoveryCode(input.MFAcode) {
			res, updateErr := tx.ExecContext(r.Context(), `UPDATE mfa_recovery_codes SET used_at=UTC_TIMESTAMP(3) WHERE user_id=UNHEX(?) AND code_hash=? AND used_at IS NULL`, u.ID, hash(normalizeRecoveryCode(input.MFAcode)))
			if updateErr != nil {
				return updateErr
			}
			rows, rowsErr := res.RowsAffected()
			if rowsErr != nil {
				return rowsErr
			}
			valid = rows == 1
		}
		if !valid {
			return bad(http.StatusUnauthorized, "MFA_INVALID_CODE", "动态验证码或未使用的恢复码错误")
		}
		if usedTOTP {
			if _, err = tx.ExecContext(r.Context(), `UPDATE user_mfa SET last_totp_step=? WHERE user_id=UNHEX(?)`, acceptedStep, u.ID); err != nil {
				return err
			}
		}
	}

	ownedTeams, err := tx.QueryContext(r.Context(), `SELECT HEX(m.team_id) FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.user_id=UNHEX(?) AND m.role='team_owner' AND m.status<>'left' AND t.status<>'deleted' FOR UPDATE`, u.ID)
	if err != nil {
		return err
	}
	owned := make([]string, 0)
	for ownedTeams.Next() {
		var teamID string
		if err = ownedTeams.Scan(&teamID); err != nil {
			ownedTeams.Close()
			return err
		}
		owned = append(owned, teamID)
	}
	err = ownedTeams.Err()
	ownedTeams.Close()
	if err != nil {
		return err
	}
	if len(owned) > 0 {
		return bad(http.StatusConflict, "TEAM_OWNERSHIP_TRANSFER_REQUIRED", "请先把所有团队所有权转移给其他有效成员，再注销账号")
	}

	var platformRole sql.NullString
	if err = tx.QueryRowContext(r.Context(), `SELECT role FROM platform_roles WHERE user_id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&platformRole); err != nil && err != sql.ErrNoRows {
		return err
	}
	if platformRole.Valid && platformRole.String == "super_admin" {
		adminRows, queryErr := tx.QueryContext(r.Context(), `SELECT HEX(p.user_id),u.status FROM platform_roles p JOIN users u ON u.id=p.user_id WHERE p.role='super_admin' ORDER BY p.user_id FOR UPDATE`)
		if queryErr != nil {
			return queryErr
		}
		activeAdmins := 0
		for adminRows.Next() {
			var adminID, adminStatus string
			if queryErr = adminRows.Scan(&adminID, &adminStatus); queryErr != nil {
				adminRows.Close()
				return queryErr
			}
			if adminStatus == "active" {
				activeAdmins++
			}
		}
		queryErr = adminRows.Err()
		adminRows.Close()
		if queryErr != nil {
			return queryErr
		}
		if activeAdmins <= 1 {
			return bad(http.StatusConflict, "LAST_SUPER_ADMIN", "这是当前唯一的平台超管，请先任命并验证另一位超管")
		}
	}

	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),'user.account_deleted','user',UNHEX(?),JSON_OBJECT('privateDataPurged',true,'sharedTeamData','retained_anonymized'))`, u.ID, u.ID); err != nil {
		return err
	}

	channelRows, err := tx.QueryContext(r.Context(), `SELECT HEX(channel_account_id) FROM channel_bindings WHERE user_id=UNHEX(?) FOR UPDATE`, u.ID)
	if err != nil {
		return err
	}
	channelIDs := make([]string, 0)
	for channelRows.Next() {
		var channelID string
		if err = channelRows.Scan(&channelID); err != nil {
			channelRows.Close()
			return err
		}
		channelIDs = append(channelIDs, channelID)
	}
	err = channelRows.Err()
	channelRows.Close()
	if err != nil {
		return err
	}

	// Delete recipient content before its subscription and notification foreign keys.
	deletes := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM agent_workspace_files WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM notification_deliveries WHERE recipient_user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM notifications WHERE recipient_user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM notification_push_subscriptions WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM notification_native_devices WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM notification_preferences WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM scheduled_jobs WHERE owner_user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM task_collaborators WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`UPDATE tasks SET assignee_id=NULL WHERE assignee_id=UNHEX(?)`, []any{u.ID}},
		{`UPDATE team_invitations SET revoked_at=COALESCE(revoked_at,UTC_TIMESTAMP(3)) WHERE created_by=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM channel_message_deliveries WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM channel_conversations WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM channel_bindings WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM agent_tool_calls WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM agent_runs WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM conversations WHERE owner_user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM user_memories WHERE owner_user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM email_change_requests WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM password_resets WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM auth_mfa_challenges WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM mfa_recovery_codes WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM user_mfa WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM auth_sessions WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM team_memberships WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM platform_roles WHERE user_id=UNHEX(?)`, []any{u.ID}},
		{`DELETE FROM login_failures WHERE email=?`, []any{email}},
	}
	for _, item := range deletes {
		if _, err = tx.ExecContext(r.Context(), item.query, item.args...); err != nil {
			return err
		}
	}
	for _, channelID := range channelIDs {
		var activeBindings int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM channel_bindings WHERE channel_account_id=UNHEX(?) AND unbound_at IS NULL`, channelID).Scan(&activeBindings); err != nil {
			return err
		}
		if activeBindings == 0 {
			for _, query := range []string{
				`DELETE FROM channel_conversations WHERE channel_account_id=UNHEX(?)`,
				`DELETE FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?)`,
				`DELETE FROM channel_sync_states WHERE channel_account_id=UNHEX(?)`,
				`DELETE FROM channel_bindings WHERE channel_account_id=UNHEX(?)`,
				`DELETE FROM channel_accounts WHERE id=UNHEX(?)`,
			} {
				if _, err = tx.ExecContext(r.Context(), query, channelID); err != nil {
					return err
				}
			}
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE users SET email=CONCAT('deleted+',LOWER(HEX(id)),'@invalid.teammind'),display_name='已注销用户',password_hash=?,status='disabled',timezone='Asia/Shanghai',deleted_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?)`, deletionPassword, u.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]any{"deleted": true, "sessionsRevoked": true, "privateDataPurged": true, "sharedTeamData": "retained_anonymized"})
	return nil
}
