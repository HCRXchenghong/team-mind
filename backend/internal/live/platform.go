package live

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
)

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request, tid string) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	rows, e := s.DB.QueryContext(r.Context(), `SELECT id,HEX(actor_user_id),action,resource_type,HEX(resource_id),created_at FROM audit_logs WHERE team_id=UNHEX(?) ORDER BY id DESC LIMIT 100`, tid)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var id int64
		var actor, action, typ, rid string
		var at time.Time
		if e = rows.Scan(&id, &actor, &action, &typ, &rid, &at); e != nil {
			return e
		}
		out = append(out, map[string]any{"id": id, "actorId": actor, "action": action, "resourceType": typ, "resourceId": rid, "createdAt": at})
	}
	write(w, 200, out)
	return rows.Err()
}
func (s *Server) platform(w http.ResponseWriter, r *http.Request, u User, path string) error {
	if !u.SuperAdmin {
		return bad(403, "SUPER_ADMIN_REQUIRED", "需要平台超管权限")
	}
	if path == "platform/security-alerts" {
		if e := method(r, "GET"); e != nil {
			return e
		}
		rows, e := s.DB.QueryContext(r.Context(), `SELECT LEFT(HEX(scope_hash),16),request_count,window_started_at,blocked_until,updated_at FROM auth_ip_rate_limits WHERE blocked_until>UTC_TIMESTAMP(3) ORDER BY blocked_until DESC LIMIT 100`)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var sourceRef string
			var count uint32
			var startedAt, blockedUntil, updatedAt time.Time
			if e = rows.Scan(&sourceRef, &count, &startedAt, &blockedUntil, &updatedAt); e != nil {
				return e
			}
			out = append(out, map[string]any{"sourceRef": sourceRef, "requestCount": count, "windowStartedAt": startedAt, "blockedUntil": blockedUntil, "updatedAt": updatedAt})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if path == "platform/audit-log" {
		if e := method(r, "GET"); e != nil {
			return e
		}
		rows, e := s.DB.QueryContext(r.Context(), `SELECT a.id,HEX(a.actor_user_id),a.action,a.resource_type,COALESCE(HEX(a.resource_id),''),HEX(t.id),t.name,COALESCE(t.review_reason,''),a.created_at FROM audit_logs a JOIN teams t ON t.id=a.team_id WHERE a.action IN ('team.review.approved','team.review.rejected') ORDER BY a.id DESC LIMIT 100`)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id int64
			var actor, action, typ, rid, teamID, teamName, reason string
			var at time.Time
			if e = rows.Scan(&id, &actor, &action, &typ, &rid, &teamID, &teamName, &reason, &at); e != nil {
				return e
			}
			out = append(out, map[string]any{"id": id, "actorId": actor, "action": action, "resourceType": typ, "resourceId": rid, "teamId": teamID, "teamName": teamName, "reason": reason, "createdAt": at})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if path == "platform/teams" && r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(id),name,status,created_at,COALESCE(review_reason,'') FROM teams ORDER BY created_at DESC LIMIT 100`)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, name, status, reason string
			var at time.Time
			if e = rows.Scan(&id, &name, &status, &at, &reason); e != nil {
				return e
			}
			out = append(out, map[string]any{"id": id, "name": name, "status": status, "createdAt": at, "reviewReason": reason})
		}
		write(w, 200, out)
		return rows.Err()
	}
	parts := strings.Split(path, "/")
	if len(parts) == 4 && parts[1] == "teams" && parts[3] == "decision" && r.Method == "POST" {
		teamID := parts[2]
		if !validID(teamID) {
			return bad(400, "INVALID_ID", "团队 ID 无效")
		}
		var input struct {
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}
		if e := body(w, r, &input); e != nil {
			return e
		}
		if input.Decision != "approved" && input.Decision != "rejected" {
			return bad(400, "VALIDATION_ERROR", "审核决定无效")
		}
		input.Reason = strings.TrimSpace(input.Reason)
		if len([]rune(input.Reason)) > 2000 {
			return bad(400, "VALIDATION_ERROR", "说明过长")
		}
		if input.Decision == "rejected" && len([]rune(input.Reason)) < 5 {
			return bad(400, "VALIDATION_ERROR", "拒绝时请填写至少 5 个字的原因")
		}
		status := "active"
		if input.Decision == "rejected" {
			status = "suspended"
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var creatorID string
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(created_by) FROM teams WHERE id=UNHEX(?) AND status='pending_review' FOR UPDATE`, teamID).Scan(&creatorID)
		if e == sql.ErrNoRows {
			return bad(409, "REVIEW_CONFLICT", "团队已被处理或不存在")
		}
		if e != nil {
			return e
		}
		res, e := tx.ExecContext(r.Context(), `UPDATE teams SET status=?,approved_by=UNHEX(?),approved_at=UTC_TIMESTAMP(3),review_reason=? WHERE id=UNHEX(?) AND status='pending_review'`, status, u.ID, input.Reason, teamID)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(409, "REVIEW_CONFLICT", "团队已被处理或不存在")
		}
		if e = addAudit(tx, u.ID, teamID, "team.review."+input.Decision, "team", teamID); e != nil {
			return e
		}
		title := "团队申请已批准"
		body := input.Reason
		if body == "" {
			body = "你的团队现在可以使用 TeamMind。"
		}
		if input.Decision == "rejected" {
			title = "团队申请未通过"
			if body == "你的团队现在可以使用 TeamMind。" {
				body = "请联系平台管理员了解审核结果。"
			}
		}
		if e = notifyResource(tx, teamID, creatorID, "team_review", title, body, "team", teamID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]string{"status": status})
		return nil
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}
