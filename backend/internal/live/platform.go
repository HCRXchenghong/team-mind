package live

import (
	"database/sql"
	"net/http"
	"net/mail"
	"os"
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
	if strings.HasPrefix(path, "platform/ai/") {
		return s.aiAdmin(w, r, u, path)
	}
	if path == "platform/company-invites" || strings.HasPrefix(path, "platform/company-invites/") {
		return s.companyInvites(w, r, u, path)
	}
	switch path {
	case "platform/metrics":
		return s.platformMetrics(w, r)
	case "platform/overview":
		return s.platformOverview(w, r)
	case "platform/readiness":
		return s.platformReadiness(w, r)
	case "platform/readiness/test-email":
		return s.platformTestEmail(w, r)
	case "platform/security-alerts":
		return s.platformSecurityAlerts(w, r)
	case "platform/audit-log":
		return s.platformAuditLog(w, r)
	case "platform/teams":
		return s.platformTeams(w, r)
	case "platform/settings":
		return s.platformSettings(w, r, u)
	}
	parts := strings.Split(path, "/")
	if len(parts) == 4 && parts[1] == "teams" && parts[3] == "decision" && r.Method == "POST" {
		return s.platformTeamDecision(w, r, u, parts[2])
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}

// platformOverview returns the counters an operator needs before drilling into
// any table. Keeping this in one query avoids the UI guessing from capped lists.
func (s *Server) platformOverview(w http.ResponseWriter, r *http.Request) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	var total, pending, active, suspended int
	if e := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*),
		COALESCE(SUM(status='pending_review'),0),
		COALESCE(SUM(status='active'),0),
		COALESCE(SUM(status='suspended'),0)
		FROM teams WHERE deleted_at IS NULL`).Scan(&total, &pending, &active, &suspended); e != nil {
		return e
	}
	var reviews int
	if e := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM audit_logs WHERE action IN ('team.review.approved','team.review.rejected')`).Scan(&reviews); e != nil {
		return e
	}
	var alerts int
	if e := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM auth_ip_rate_limits WHERE blocked_until>UTC_TIMESTAMP(3)`).Scan(&alerts); e != nil {
		return e
	}
	write(w, 200, map[string]any{
		"teams":  map[string]int{"total": total, "pending": pending, "active": active, "suspended": suspended},
		"audit":  map[string]int{"total": reviews},
		"alerts": map[string]int{"active": alerts},
	})
	return nil
}

func (s *Server) platformSecurityAlerts(w http.ResponseWriter, r *http.Request) error {
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

func (s *Server) platformTeams(w http.ResponseWriter, r *http.Request) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if status != "" && !in(status, "pending_review", "active", "suspended") {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "团队状态筛选无效")
	}
	if kind != "" && kind != "team" && kind != "company" {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "组织类型筛选无效")
	}
	where := ` WHERE t.deleted_at IS NULL`
	args := []any{}
	if q != "" {
		where += ` AND (t.name LIKE CONCAT('%',?,'%') OR u.display_name LIKE CONCAT('%',?,'%') OR u.email LIKE CONCAT('%',?,'%'))`
		args = append(args, q, q, q)
	}
	if status != "" {
		where += ` AND t.status=?`
		args = append(args, status)
	}
	if kind != "" {
		where += ` AND t.kind=?`
		args = append(args, kind)
	}
	var total int
	if e := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM teams t LEFT JOIN users u ON u.id=t.created_by`+where, args...).Scan(&total); e != nil {
		return e
	}
	query := `SELECT HEX(t.id),t.name,t.kind,t.status,t.created_at,COALESCE(t.review_reason,''),
		HEX(t.created_by),COALESCE(u.display_name,''),COALESCE(u.email,''),
		COALESCE(au.display_name,''),t.approved_at,
		(SELECT COUNT(*) FROM team_memberships m WHERE m.team_id=t.id AND m.status='active'),
		(SELECT COUNT(*) FROM projects p WHERE p.team_id=t.id AND p.deleted_at IS NULL)
		FROM teams t
		LEFT JOIN users u ON u.id=t.created_by
		LEFT JOIN users au ON au.id=t.approved_by` + where + ` ORDER BY t.created_at DESC`
	paged := r.URL.Query().Has("page")
	if paged {
		n, size := page(r)
		query += ` LIMIT ? OFFSET ?`
		args = append(args, size, (n-1)*size)
	} else {
		query += ` LIMIT 100`
	}
	rows, e := s.DB.QueryContext(r.Context(), query, args...)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, kind, statusValue, reason, ownerID, ownerName, ownerEmail, approvedByName string
		var createdAt time.Time
		var approvedAt sql.NullTime
		var memberCount, projectCount int
		if e = rows.Scan(&id, &name, &kind, &statusValue, &createdAt, &reason, &ownerID, &ownerName, &ownerEmail, &approvedByName, &approvedAt, &memberCount, &projectCount); e != nil {
			return e
		}
		item := map[string]any{
			"id": id, "name": name, "kind": kind, "status": statusValue, "createdAt": createdAt, "reviewReason": reason,
			"ownerId": ownerID, "ownerName": ownerName, "ownerEmail": ownerEmail,
			"approvedByName": approvedByName, "memberCount": memberCount, "projectCount": projectCount,
		}
		if approvedAt.Valid {
			item["approvedAt"] = approvedAt.Time
		}
		items = append(items, item)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if paged {
		n, size := page(r)
		write(w, 200, map[string]any{"items": items, "total": total, "page": n, "pageSize": size})
		return nil
	}
	write(w, 200, items)
	return nil
}

func (s *Server) platformAuditLog(w http.ResponseWriter, r *http.Request) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	var total int
	if e := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM audit_logs WHERE action IN ('team.review.approved','team.review.rejected')`).Scan(&total); e != nil {
		return e
	}
	query := `SELECT a.id,a.action,a.created_at,
		HEX(a.actor_user_id),COALESCE(u.display_name,''),COALESCE(u.email,''),
		HEX(t.id),t.name,
		COALESCE(JSON_UNQUOTE(JSON_EXTRACT(a.after_summary,'$.reason')),'')
		FROM audit_logs a
		JOIN teams t ON t.id=a.team_id
		LEFT JOIN users u ON u.id=a.actor_user_id
		WHERE a.action IN ('team.review.approved','team.review.rejected')
		ORDER BY a.id DESC`
	paged := r.URL.Query().Has("page")
	args := []any{}
	if paged {
		n, size := page(r)
		query += ` LIMIT ? OFFSET ?`
		args = append(args, size, (n-1)*size)
	} else {
		query += ` LIMIT 100`
	}
	rows, e := s.DB.QueryContext(r.Context(), query, args...)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var action, actorID, actorName, actorEmail, teamID, teamName, reason string
		var at time.Time
		if e = rows.Scan(&id, &action, &at, &actorID, &actorName, &actorEmail, &teamID, &teamName, &reason); e != nil {
			return e
		}
		items = append(items, map[string]any{
			"id": id, "action": action, "actorId": actorID, "actorName": actorName, "actorEmail": actorEmail,
			"teamId": teamID, "teamName": teamName, "reason": reason, "createdAt": at,
		})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if paged {
		n, size := page(r)
		write(w, 200, map[string]any{"items": items, "total": total, "page": n, "pageSize": size})
		return nil
	}
	write(w, 200, items)
	return nil
}

func (s *Server) platformTeamDecision(w http.ResponseWriter, r *http.Request, u User, teamID string) error {
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
		return bad(409, "REVIEW_CONFLICT", "公司已被处理或不存在")
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
		return bad(409, "REVIEW_CONFLICT", "公司已被处理或不存在")
	}
	if e = addAuditWithSummary(tx, u.ID, teamID, "team.review."+input.Decision, "team", teamID, map[string]any{"reason": input.Reason}); e != nil {
		return e
	}
	title := "团队申请已批准"
	body := input.Reason
	if body == "" {
		body = "你的公司现在可以使用 TeamMind。"
	}
	if input.Decision == "rejected" {
		title = "团队申请未通过"
		if body == "你的公司现在可以使用 TeamMind。" {
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

// platformReadiness exposes launch blockers without leaking secrets. It is
// intentionally super-admin-only; production checks can call it after login.
func (s *Server) platformReadiness(w http.ResponseWriter, r *http.Request) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	var migrated int
	dbOK := s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM schema_migrations WHERE version='000043_ai_provider_created_by_nullable'").Scan(&migrated) == nil && migrated == 1
	var globalProviders, teamProviders int
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(team_id IS NULL),0),COALESCE(SUM(team_id IS NOT NULL),0) FROM ai_providers WHERE enabled=1`).Scan(&globalProviders, &teamProviders)
	emailOK := notificationEmailConfigured()
	pushOK := notificationPushConfigured()
	mode := wechatMode()
	wechatOK := mode == "real" && os.Getenv("WECHAT_GATEWAY_URL") != "" && os.Getenv("WECHAT_GATEWAY_BRIDGE_TOKEN") != "" && len(wechatKey()) == 32
	sttOK := sttConfigured()
	publicURL := strings.TrimSpace(os.Getenv("PUBLIC_URL"))
	missing := []string{}
	if !dbOK {
		missing = append(missing, "database_migration")
	}
	if globalProviders+teamProviders == 0 {
		missing = append(missing, "ai_provider")
	}
	if !emailOK {
		missing = append(missing, "smtp")
	}
	if !wechatOK {
		missing = append(missing, "wechat")
	}
	if !sttOK {
		missing = append(missing, "stt")
	}
	if publicURL == "" {
		missing = append(missing, "public_url")
	}
	status := "ready"
	if len(missing) > 0 {
		status = "degraded"
	}
	write(w, http.StatusOK, map[string]any{
		"status": status,
		"checks": map[string]any{
			"databaseMigration": dbOK,
			"globalAIProviders": globalProviders,
			"teamAIProviders":   teamProviders,
			"smtp":              emailOK,
			"webPush":           pushOK,
			"wechatMode":        mode,
			"wechatGateway":     wechatOK,
			"stt":               sttOK,
			"publicURL":         publicURL != "",
		},
		"missing": missing,
	})
	return nil
}

func (s *Server) platformTestEmail(w http.ResponseWriter, r *http.Request) error {
	if e := method(r, "POST"); e != nil {
		return e
	}
	var input struct {
		To string `json:"to"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	to := strings.TrimSpace(input.To)
	if _, err := mail.ParseAddress(to); err != nil {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "收件邮箱无效")
	}
	if !notificationEmailConfigured() {
		return bad(http.StatusServiceUnavailable, "SMTP_NOT_CONFIGURED", "SMTP 尚未配置")
	}
	if err := sendNotificationEmail(r.Context(), to, strings.TrimSpace(os.Getenv("PUBLIC_URL")), 1); err != nil {
		return bad(http.StatusBadGateway, "SMTP_SEND_FAILED", err.Error())
	}
	write(w, http.StatusOK, map[string]bool{"sent": true})
	return nil
}
