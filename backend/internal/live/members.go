package live

import (
	"database/sql"
	"net/http"
	"time"
)

func (s *Server) members(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {
	if len(p) == 3 && r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(u.id),u.display_name,u.email,m.role,m.status,m.joined_at FROM team_memberships m JOIN users u ON u.id=m.user_id WHERE m.team_id=UNHEX(?) AND m.status IN ('active','suspended') ORDER BY m.joined_at`, tid)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var uid, name, email, memberRole, status string
			var joined sql.NullTime
			if e = rows.Scan(&uid, &name, &email, &memberRole, &status, &joined); e != nil {
				return e
			}
			out = append(out, map[string]any{"id": uid, "name": name, "email": email, "role": memberRole, "status": status, "joinedAt": joined.Time})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if len(p) != 4 || r.Method != "PATCH" {
		return bad(404, "NOT_FOUND", "资源不存在")
	}
	if e := admin(role); e != nil {
		return e
	}
	target := p[3]
	if !validID(target) {
		return bad(400, "INVALID_ID", "成员 ID 无效")
	}
	var x struct {
		Role   string `json:"role"`
		Status string `json:"status"`
	}
	if e := body(w, r, &x); e != nil {
		return e
	}
	if x.Role != "" && x.Role != "member" && x.Role != "team_admin" && x.Role != "team_owner" {
		return bad(400, "VALIDATION_ERROR", "角色无效")
	}
	if x.Status != "" && x.Status != "active" && x.Status != "suspended" && x.Status != "left" {
		return bad(400, "VALIDATION_ERROR", "状态无效")
	}
	if target == u.ID && (x.Status != "" || x.Role != "") {
		return bad(400, "SELF_CHANGE_DENIED", "请由另一位所有者处理你的成员身份")
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Recheck the actor under a row lock. A simultaneous ownership transfer or
	// suspension must revoke this request's authority before it can write.
	actingRole, e := activeTeamRoleTx(r.Context(), tx, tid, u)
	if e == sql.ErrNoRows {
		return bad(403, "TEAM_ROLE_CHANGED", "你的团队权限已变化，请刷新后重试")
	}
	if e != nil {
		return e
	}
	if actingRole != role {
		return bad(403, "TEAM_ROLE_CHANGED", "你的团队权限已变化，请刷新后重试")
	}
	var current, currentStatus string
	e = tx.QueryRowContext(r.Context(), `SELECT role,status FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND status IN ('active','suspended') FOR UPDATE`, tid, target).Scan(&current, &currentStatus)
	if e == sql.ErrNoRows {
		return bad(404, "NOT_FOUND", "成员不存在")
	}
	if e != nil {
		return e
	}
	if role == "team_admin" && current != "member" {
		return bad(403, "TEAM_OWNER_REQUIRED", "团队管理员只能管理普通成员")
	}
	if role == "team_admin" && x.Role == "team_admin" {
		return bad(403, "TEAM_OWNER_REQUIRED", "只有团队所有者可以任命管理员")
	}
	if current == "team_owner" || x.Role == "team_owner" {
		if e = owner(role); e != nil {
			return e
		}
		if x.Role == "team_owner" {
			if currentStatus != "active" || (x.Status != "" && x.Status != "active") {
				return bad(409, "OWNER_MUST_BE_ACTIVE", "所有权只能转移给有效成员")
			}
			if role != "team_owner" || current == "team_owner" {
				return bad(409, "OWNER_CONFLICT", "所有权状态已变化")
			}
			_, e = tx.ExecContext(r.Context(), `UPDATE team_memberships SET role='team_admin',version=version+1 WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, tid, u.ID)
			if e != nil {
				return e
			}
		} else {
			return bad(400, "OWNER_REQUIRED", "请先转移所有权")
		}
	}
	if x.Role == "" {
		x.Role = current
	}
	if x.Status == "" {
		x.Status = currentStatus
	}
	_, e = tx.ExecContext(r.Context(), `UPDATE team_memberships SET role=?,status=?,version=version+1 WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, x.Role, x.Status, tid, target)
	if e != nil {
		return e
	}
	if e = addAudit(tx, u.ID, tid, "member.update", "user", target); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	write(w, 200, map[string]bool{"saved": true})
	return nil
}
func (s *Server) invitations(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {
	if e := admin(role); e != nil {
		return e
	}
	if len(p) == 3 && r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(id),expires_at,max_uses,used_count,revoked_at,created_at FROM team_invitations WHERE team_id=UNHEX(?) ORDER BY created_at DESC LIMIT 100`, tid)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var iid string
			var expiry, created time.Time
			var max, used int
			var revoked sql.NullTime
			if e = rows.Scan(&iid, &expiry, &max, &used, &revoked, &created); e != nil {
				return e
			}
			out = append(out, map[string]any{"id": iid, "expiresAt": expiry, "maxUses": max, "usedCount": used, "revoked": revoked.Valid, "createdAt": created})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if len(p) == 3 && r.Method == "POST" {
		var x struct {
			ExpiresInHours int `json:"expiresInHours"`
			MaxUses        int `json:"maxUses"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if x.ExpiresInHours < 1 || x.ExpiresInHours > 720 || x.MaxUses < 1 || x.MaxUses > 100 {
			return bad(400, "VALIDATION_ERROR", "有效期需在 1–720 小时，次数需在 1–100 次")
		}
		t := token()
		iid := id()
		exp := time.Now().UTC().Add(time.Duration(x.ExpiresInHours) * time.Hour)
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO team_invitations(id,team_id,token_hash,role,created_by,expires_at,max_uses) VALUES(UNHEX(?),UNHEX(?),?,'member',UNHEX(?),?,?)`, iid, tid, hash(t), u.ID, exp, x.MaxUses)
		if e != nil {
			return e
		}
		if e = addAudit(tx, u.ID, tid, "invitation.create", "invitation", iid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 201, map[string]any{"id": iid, "token": t, "expiresAt": exp, "maxUses": x.MaxUses})
		return nil
	}
	if len(p) == 4 && r.Method == "DELETE" {
		iid := p[3]
		if !validID(iid) {
			return bad(400, "INVALID_ID", "邀请 ID 无效")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		res, e := tx.ExecContext(r.Context(), `UPDATE team_invitations SET revoked_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND revoked_at IS NULL`, iid, tid)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(404, "NOT_FOUND", "邀请不存在或已撤销")
		}
		if e = addAudit(tx, u.ID, tid, "invitation.revoke", "invitation", iid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"revoked": true})
		return nil
	}
	return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}
func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request, u User) error {
	if e := method(r, "POST"); e != nil {
		return e
	}
	var x struct {
		Token string `json:"token"`
	}
	if e := body(w, r, &x); e != nil {
		return e
	}
	if len(x.Token) != 64 {
		return bad(400, "INVALID_INVITATION", "邀请链接无效")
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var tid, iid string
	var max, used int
	var expiry time.Time
	var revoked sql.NullTime
	e = tx.QueryRowContext(r.Context(), `SELECT HEX(i.id),HEX(i.team_id),i.max_uses,i.used_count,i.expires_at,i.revoked_at FROM team_invitations i JOIN teams t ON t.id=i.team_id WHERE i.token_hash=? AND t.status='active' FOR UPDATE`, hash(x.Token)).Scan(&iid, &tid, &max, &used, &expiry, &revoked)
	if e == sql.ErrNoRows {
		return bad(404, "INVALID_INVITATION", "邀请链接无效")
	}
	if e != nil {
		return e
	}
	if revoked.Valid || time.Now().UTC().After(expiry) || used >= max {
		return bad(410, "INVITATION_EXPIRED", "邀请已过期、撤销或用尽")
	}
	var existing string
	e = tx.QueryRowContext(r.Context(), `SELECT status FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) FOR UPDATE`, tid, u.ID).Scan(&existing)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if existing == "active" {
		write(w, 200, map[string]any{"teamId": tid, "alreadyMember": true})
		return nil
	}
	if existing != "" {
		_, e = tx.ExecContext(r.Context(), `UPDATE team_memberships SET status='active',joined_at=UTC_TIMESTAMP(3),role='member',version=version+1 WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, tid, u.ID)
	} else {
		_, e = tx.ExecContext(r.Context(), `INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'member','active',UTC_TIMESTAMP(3))`, tid, u.ID)
	}
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(r.Context(), `UPDATE team_invitations SET used_count=used_count+1 WHERE id=UNHEX(?)`, iid)
	if e != nil {
		return e
	}
	if e = addAudit(tx, u.ID, tid, "invitation.accept", "invitation", iid); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	write(w, 200, map[string]any{"teamId": tid, "alreadyMember": false})
	return nil
}
