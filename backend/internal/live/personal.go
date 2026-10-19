package live

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
)

func (s *Server) profile(w http.ResponseWriter, r *http.Request, u User) error {
	if e := method(r, "PATCH"); e != nil {
		return e
	}
	var x struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	if e := body(w, r, &x); e != nil {
		return e
	}
	x.Name = strings.TrimSpace(x.Name)
	if len([]rune(x.Name)) < 2 || len([]rune(x.Name)) > 120 {
		return bad(400, "VALIDATION_ERROR", "姓名需为 2–120 字")
	}
	if _, e := time.LoadLocation(x.Timezone); e != nil {
		return bad(400, "VALIDATION_ERROR", "时区无效")
	}
	_, e := s.DB.ExecContext(r.Context(), `UPDATE users SET display_name=?,timezone=? WHERE id=UNHEX(?)`, x.Name, x.Timezone, u.ID)
	if e != nil {
		return e
	}
	write(w, 200, map[string]bool{"saved": true})
	return nil
}

func (s *Server) notificationPreferences(w http.ResponseWriter, r *http.Request, u User) error {
	if r.Method == "GET" {
		var enabled bool
		var emailEnabled, pushEnabled, nativePushEnabled bool
		var start, end sql.NullString
		e := s.DB.QueryRowContext(r.Context(), `SELECT enabled,email_enabled,push_enabled,native_push_enabled,TIME_FORMAT(quiet_start,'%H:%i'),TIME_FORMAT(quiet_end,'%H:%i') FROM notification_preferences WHERE user_id=UNHEX(?)`, u.ID).Scan(&enabled, &emailEnabled, &pushEnabled, &nativePushEnabled, &start, &end)
		if e == sql.ErrNoRows {
			write(w, 200, map[string]any{"enabled": true, "emailEnabled": false, "pushEnabled": false, "nativePushEnabled": false, "quietStart": "", "quietEnd": ""})
			return nil
		}
		if e != nil {
			return e
		}
		write(w, 200, map[string]any{"enabled": enabled, "emailEnabled": emailEnabled, "pushEnabled": pushEnabled, "nativePushEnabled": nativePushEnabled, "quietStart": start.String, "quietEnd": end.String})
		return nil
	}
	if e := method(r, "PATCH"); e != nil {
		return e
	}
	var x struct {
		Enabled           bool   `json:"enabled"`
		EmailEnabled      bool   `json:"emailEnabled"`
		PushEnabled       bool   `json:"pushEnabled"`
		NativePushEnabled bool   `json:"nativePushEnabled"`
		QuietStart        string `json:"quietStart"`
		QuietEnd          string `json:"quietEnd"`
	}
	if e := body(w, r, &x); e != nil {
		return e
	}
	var previousEmailEnabled, previousPushEnabled, previousNativePushEnabled bool
	prefErr := s.DB.QueryRowContext(r.Context(), `SELECT email_enabled,push_enabled,native_push_enabled FROM notification_preferences WHERE user_id=UNHEX(?)`, u.ID).Scan(&previousEmailEnabled, &previousPushEnabled, &previousNativePushEnabled)
	if prefErr != nil && prefErr != sql.ErrNoRows {
		return prefErr
	}
	if (x.QuietStart == "") != (x.QuietEnd == "") {
		return bad(400, "VALIDATION_ERROR", "免打扰开始和结束时间必须同时填写或同时留空")
	}
	if x.QuietStart != "" && x.QuietStart == x.QuietEnd {
		return bad(400, "VALIDATION_ERROR", "免打扰开始和结束时间不能相同")
	}
	if x.QuietStart != "" {
		if _, e := time.Parse("15:04", x.QuietStart); e != nil {
			return bad(400, "VALIDATION_ERROR", "免打扰开始时间无效")
		}
	}
	if x.QuietEnd != "" {
		if _, e := time.Parse("15:04", x.QuietEnd); e != nil {
			return bad(400, "VALIDATION_ERROR", "免打扰结束时间无效")
		}
	}
	if x.EmailEnabled && !previousEmailEnabled && !notificationEmailConfigured() {
		return bad(409, "EMAIL_CHANNEL_NOT_CONFIGURED", "邮件通知服务尚未配置，请联系部署管理员")
	}
	if x.PushEnabled && !previousPushEnabled {
		if _, err := notificationPushKeys(); err != nil {
			return bad(409, "PUSH_CHANNEL_NOT_CONFIGURED", "浏览器推送服务尚未配置，请联系部署管理员")
		}
		var subscriptions int
		if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notification_push_subscriptions WHERE user_id=UNHEX(?) AND status='active'`, u.ID).Scan(&subscriptions); err != nil {
			return err
		}
		if subscriptions == 0 {
			return bad(409, "PUSH_SUBSCRIPTION_REQUIRED", "请先在此设备启用浏览器推送")
		}
	}
	if x.NativePushEnabled && !previousNativePushEnabled {
		if !nativePushConfigured() {
			return bad(409, "NATIVE_PUSH_NOT_CONFIGURED", "原生推送服务尚未配置，请联系部署管理员")
		}
		var devices int
		if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notification_native_devices WHERE user_id=UNHEX(?) AND status='active'`, u.ID).Scan(&devices); err != nil {
			return err
		}
		if devices == 0 {
			return bad(409, "NATIVE_DEVICE_REQUIRED", "请先在 TeamMind 原生应用中启用此设备推送")
		}
	}
	var start, end any
	if x.QuietStart != "" {
		start = x.QuietStart
	}
	if x.QuietEnd != "" {
		end = x.QuietEnd
	}
	_, e := s.DB.ExecContext(r.Context(), `INSERT INTO notification_preferences(user_id,enabled,email_enabled,push_enabled,native_push_enabled,quiet_start,quiet_end) VALUES(UNHEX(?),?,?,?,?,?,?) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),email_enabled=VALUES(email_enabled),push_enabled=VALUES(push_enabled),native_push_enabled=VALUES(native_push_enabled),quiet_start=VALUES(quiet_start),quiet_end=VALUES(quiet_end)`, u.ID, x.Enabled, x.EmailEnabled, x.PushEnabled, x.NativePushEnabled, start, end)
	if e != nil {
		return e
	}
	write(w, 200, map[string]bool{"saved": true})
	return nil
}

func (s *Server) memories(w http.ResponseWriter, r *http.Request, u User, path string) error {
	p := strings.Split(path, "/")
	if len(p) == 2 && r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(m.id),m.category,m.content,m.confirmed_at,m.updated_at,m.review_at,m.expires_at,HEX(m.source_message_id),HEX(cm.conversation_id),cm.role,LEFT(cm.content,240) FROM user_memories m LEFT JOIN conversation_messages cm ON cm.id=m.source_message_id WHERE m.owner_user_id=UNHEX(?) AND m.deleted_at IS NULL ORDER BY m.updated_at DESC`, u.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var mid, category, content string
			var confirmed, reviewAt, expiresAt sql.NullTime
			var sourceMessage, sourceConversation, sourceRole, sourceExcerpt sql.NullString
			var updated time.Time
			if e = rows.Scan(&mid, &category, &content, &confirmed, &updated, &reviewAt, &expiresAt, &sourceMessage, &sourceConversation, &sourceRole, &sourceExcerpt); e != nil {
				return e
			}
			out = append(out, map[string]any{"id": mid, "category": category, "content": content, "confirmed": confirmed.Valid, "updatedAt": updated, "reviewAt": nullTimeString(reviewAt), "expiresAt": nullTimeString(expiresAt), "sourceMessageId": sourceMessage.String, "sourceConversationId": sourceConversation.String, "sourceRole": sourceRole.String, "sourceExcerpt": sourceExcerpt.String})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if len(p) == 2 && r.Method == "POST" {
		var x struct {
			Category  string `json:"category"`
			Content   string `json:"content"`
			ReviewAt  string `json:"reviewAt"`
			ExpiresAt string `json:"expiresAt"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if !in(x.Category, "profile", "experience", "current_work", "operational_knowledge") || strings.TrimSpace(x.Content) == "" || len([]rune(x.Content)) > 10000 {
			return bad(400, "VALIDATION_ERROR", "记忆内容或类型无效")
		}
		reviewAt, expiresAt, e := memoryDates(x.ReviewAt, x.ExpiresAt)
		if e != nil {
			return e
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var locked string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM users WHERE id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&locked); e != nil {
			return e
		}
		var duplicate string
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM user_memories WHERE owner_user_id=UNHEX(?) AND deleted_at IS NULL AND LOWER(TRIM(content))=LOWER(TRIM(?)) LIMIT 1`, u.ID, strings.TrimSpace(x.Content)).Scan(&duplicate)
		if e == nil {
			return bad(409, "MEMORY_DUPLICATE", "已有相同记忆，请编辑现有条目或调整内容")
		}
		if e != sql.ErrNoRows {
			return e
		}
		mid := id()
		_, e = tx.ExecContext(r.Context(), `INSERT INTO user_memories(id,owner_user_id,category,content,review_at,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?,?,?)`, mid, u.ID, x.Category, strings.TrimSpace(x.Content), reviewAt, expiresAt)
		if e != nil {
			return e
		}
		if e = addAudit(tx, u.ID, "", "memory.create", "user_memory", mid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 201, map[string]string{"id": mid})
		return nil
	}
	if len(p) != 3 || !validID(p[2]) {
		return bad(404, "NOT_FOUND", "记忆不存在")
	}
	mid := p[2]
	if r.Method == "DELETE" {
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var locked string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM users WHERE id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&locked); e != nil {
			return e
		}
		res, e := tx.ExecContext(r.Context(), `UPDATE user_memories SET deleted_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?) AND deleted_at IS NULL`, mid, u.ID)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(404, "NOT_FOUND", "记忆不存在")
		}
		if e = addAudit(tx, u.ID, "", "memory.delete", "user_memory", mid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"deleted": true})
		return nil
	}
	if r.Method == "PATCH" {
		var x struct {
			Category  string `json:"category"`
			Content   string `json:"content"`
			Confirmed bool   `json:"confirmed"`
			ReviewAt  string `json:"reviewAt"`
			ExpiresAt string `json:"expiresAt"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if !in(x.Category, "profile", "experience", "current_work", "operational_knowledge") || strings.TrimSpace(x.Content) == "" || len([]rune(x.Content)) > 10000 {
			return bad(400, "VALIDATION_ERROR", "记忆内容或类型无效")
		}
		reviewAt, expiresAt, e := memoryDates(x.ReviewAt, x.ExpiresAt)
		if e != nil {
			return e
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var locked string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM users WHERE id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&locked); e != nil {
			return e
		}
		var duplicate string
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM user_memories WHERE owner_user_id=UNHEX(?) AND deleted_at IS NULL AND id<>UNHEX(?) AND LOWER(TRIM(content))=LOWER(TRIM(?)) LIMIT 1`, u.ID, mid, strings.TrimSpace(x.Content)).Scan(&duplicate)
		if e == nil {
			return bad(409, "MEMORY_DUPLICATE", "已有相同记忆，请编辑现有条目或调整内容")
		}
		if e != sql.ErrNoRows {
			return e
		}
		res, e := tx.ExecContext(r.Context(), `UPDATE user_memories SET category=?,content=?,confirmed_at=IF(?,COALESCE(confirmed_at,UTC_TIMESTAMP(3)),NULL),review_at=?,expires_at=? WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?) AND deleted_at IS NULL`, x.Category, strings.TrimSpace(x.Content), x.Confirmed, reviewAt, expiresAt, mid, u.ID)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(404, "NOT_FOUND", "记忆不存在")
		}
		if e = addAudit(tx, u.ID, "", "memory.update", "user_memory", mid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"saved": true})
		return nil
	}
	return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}

func nullTimeString(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time.UTC().Format(time.RFC3339)
}

func memoryDates(review, expires string) (any, any, error) {
	parse := func(value, label string) (any, error) {
		if value == "" {
			return nil, nil
		}
		date, err := time.Parse("2006-01-02", value)
		if err != nil {
			return nil, bad(400, "VALIDATION_ERROR", label+"日期无效")
		}
		return date.UTC(), nil
	}
	reviewAt, err := parse(review, "复核")
	if err != nil {
		return nil, nil, err
	}
	expiresAt, err := parse(expires, "到期")
	if err != nil {
		return nil, nil, err
	}
	if review != "" && expires != "" && review > expires {
		return nil, nil, bad(400, "VALIDATION_ERROR", "复核日期不能晚于到期日期")
	}
	return reviewAt, expiresAt, nil
}
func (s *Server) teamMemories(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {
	if len(p) == 3 && r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(m.id),m.category,m.content,m.confirmed_at,m.updated_at,m.review_at,m.expires_at,HEX(m.source_message_id),HEX(cm.conversation_id),cm.role,LEFT(cm.content,240),HEX(m.created_by) FROM team_memories m LEFT JOIN conversation_messages cm ON cm.id=m.source_message_id WHERE m.team_id=UNHEX(?) AND m.deleted_at IS NULL AND (? IN ('team_admin','team_owner') OR m.confirmed_at IS NOT NULL) ORDER BY m.updated_at DESC`, tid, role)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var mid, cat, content, createdBy string
			var confirmed, reviewAt, expiresAt sql.NullTime
			var sourceMessage, sourceConversation, sourceRole, sourceExcerpt sql.NullString
			var updated time.Time
			if e = rows.Scan(&mid, &cat, &content, &confirmed, &updated, &reviewAt, &expiresAt, &sourceMessage, &sourceConversation, &sourceRole, &sourceExcerpt, &createdBy); e != nil {
				return e
			}
			item := map[string]any{"id": mid, "category": cat, "content": content, "confirmed": confirmed.Valid, "updatedAt": updated, "reviewAt": nullTimeString(reviewAt), "expiresAt": nullTimeString(expiresAt)}
			// Team knowledge may be shared, but its source conversation remains private to its author.
			if strings.EqualFold(createdBy, u.ID) {
				item["sourceMessageId"] = sourceMessage.String
				item["sourceConversationId"] = sourceConversation.String
				item["sourceRole"] = sourceRole.String
				item["sourceExcerpt"] = sourceExcerpt.String
			}
			out = append(out, item)
		}
		write(w, 200, out)
		return rows.Err()
	}
	if e := admin(role); e != nil {
		return e
	}
	if len(p) == 3 && r.Method == "POST" {
		var x struct {
			Category  string `json:"category"`
			Content   string `json:"content"`
			ReviewAt  string `json:"reviewAt"`
			ExpiresAt string `json:"expiresAt"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if strings.TrimSpace(x.Category) == "" || len([]rune(x.Category)) > 80 || strings.TrimSpace(x.Content) == "" || len([]rune(x.Content)) > 10000 {
			return bad(400, "VALIDATION_ERROR", "知识内容无效")
		}
		reviewAt, expiresAt, e := memoryDates(x.ReviewAt, x.ExpiresAt)
		if e != nil {
			return e
		}
		mid := id()
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		var lockedTeam string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM teams WHERE id=UNHEX(?) AND status='active' FOR UPDATE`, tid).Scan(&lockedTeam); e != nil {
			return e
		}
		var duplicate string
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM team_memories WHERE team_id=UNHEX(?) AND deleted_at IS NULL AND LOWER(TRIM(content))=LOWER(TRIM(?)) LIMIT 1`, tid, strings.TrimSpace(x.Content)).Scan(&duplicate)
		if e == nil {
			return bad(409, "MEMORY_DUPLICATE", "团队知识中已有相同内容，请编辑现有条目")
		}
		if e != sql.ErrNoRows {
			return e
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO team_memories(id,team_id,created_by,category,content,review_at,expires_at) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),?,?,?,?)`, mid, tid, u.ID, strings.TrimSpace(x.Category), strings.TrimSpace(x.Content), reviewAt, expiresAt)
		if e != nil {
			return e
		}
		if e = addAudit(tx, u.ID, tid, "team_memory.create", "team_memory", mid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 201, map[string]string{"id": mid})
		return nil
	}
	if len(p) != 4 || !validID(p[3]) {
		return bad(404, "NOT_FOUND", "知识不存在")
	}
	mid := p[3]
	if r.Method == "DELETE" {
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		res, e := tx.ExecContext(r.Context(), `UPDATE team_memories SET deleted_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, mid, tid)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(404, "NOT_FOUND", "知识不存在")
		}
		if e = addAudit(tx, u.ID, tid, "team_memory.delete", "team_memory", mid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"deleted": true})
		return nil
	}
	if r.Method == "PATCH" {
		var x struct {
			Category  string `json:"category"`
			Content   string `json:"content"`
			Confirmed bool   `json:"confirmed"`
			ReviewAt  string `json:"reviewAt"`
			ExpiresAt string `json:"expiresAt"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if strings.TrimSpace(x.Category) == "" || len([]rune(x.Category)) > 80 || strings.TrimSpace(x.Content) == "" || len([]rune(x.Content)) > 10000 {
			return bad(400, "VALIDATION_ERROR", "知识内容无效")
		}
		reviewAt, expiresAt, e := memoryDates(x.ReviewAt, x.ExpiresAt)
		if e != nil {
			return e
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		var lockedTeam string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM teams WHERE id=UNHEX(?) AND status='active' FOR UPDATE`, tid).Scan(&lockedTeam); e != nil {
			return e
		}
		var existing string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM team_memories WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL FOR UPDATE`, mid, tid).Scan(&existing); e == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "知识不存在")
		} else if e != nil {
			return e
		}
		var duplicate string
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM team_memories WHERE team_id=UNHEX(?) AND deleted_at IS NULL AND id<>UNHEX(?) AND LOWER(TRIM(content))=LOWER(TRIM(?)) LIMIT 1`, tid, mid, strings.TrimSpace(x.Content)).Scan(&duplicate)
		if e == nil {
			return bad(409, "MEMORY_DUPLICATE", "团队知识中已有相同内容，请调整内容")
		}
		if e != sql.ErrNoRows {
			return e
		}
		res, e := tx.ExecContext(r.Context(), `UPDATE team_memories SET category=?,content=?,confirmed_at=IF(?,COALESCE(confirmed_at,UTC_TIMESTAMP(3)),NULL),review_at=?,expires_at=? WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, strings.TrimSpace(x.Category), strings.TrimSpace(x.Content), x.Confirmed, reviewAt, expiresAt, mid, tid)
		if e != nil {
			return e
		}
		_ = res
		if e = addAudit(tx, u.ID, tid, "team_memory.update", "team_memory", mid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"saved": true})
		return nil
	}
	return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}
func (s *Server) notifications(w http.ResponseWriter, r *http.Request, u User, path string) error {
	p := strings.Split(path, "/")
	if len(p) == 3 && p[2] == "unread-count" && r.Method == "GET" {
		var count int
		if e := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notifications WHERE recipient_user_id=UNHEX(?) AND read_at IS NULL AND visible_at<=UTC_TIMESTAMP(3)`, u.ID).Scan(&count); e != nil {
			return e
		}
		write(w, 200, map[string]int{"count": count})
		return nil
	}
	if len(p) == 2 && r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(id),HEX(team_id),type,title,body,read_at,created_at FROM notifications WHERE recipient_user_id=UNHEX(?) AND visible_at<=UTC_TIMESTAMP(3) ORDER BY created_at DESC LIMIT 100`, u.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var nid, typ, title, body string
			var team sql.NullString
			var read sql.NullTime
			var at time.Time
			if e = rows.Scan(&nid, &team, &typ, &title, &body, &read, &at); e != nil {
				return e
			}
			out = append(out, map[string]any{"id": nid, "teamId": team.String, "type": typ, "title": title, "body": body, "read": read.Valid, "createdAt": at})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if len(p) == 3 && p[2] == "read-all" && r.Method == "POST" {
		_, e := s.DB.ExecContext(r.Context(), `UPDATE notifications SET read_at=UTC_TIMESTAMP(3) WHERE recipient_user_id=UNHEX(?) AND read_at IS NULL AND visible_at<=UTC_TIMESTAMP(3)`, u.ID)
		if e != nil {
			return e
		}
		write(w, 200, map[string]bool{"saved": true})
		return nil
	}
	if len(p) == 3 && validID(p[2]) && r.Method == "PATCH" {
		res, e := s.DB.ExecContext(r.Context(), `UPDATE notifications SET read_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND recipient_user_id=UNHEX(?) AND read_at IS NULL AND visible_at<=UTC_TIMESTAMP(3)`, p[2], u.ID)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(404, "NOT_FOUND", "未读通知不存在")
		}
		write(w, 200, map[string]bool{"read": true})
		return nil
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}

func (s *Server) notificationDeliveries(w http.ResponseWriter, r *http.Request, u User) error {
	if err := method(r, "GET"); err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(id),notification_type,channel,status,created_at,delivered_at,last_error FROM notification_deliveries WHERE recipient_user_id=UNHEX(?) ORDER BY created_at DESC LIMIT 100`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var id, kind, channel, status string
		var created time.Time
		var delivered sql.NullTime
		var failure sql.NullString
		if err = rows.Scan(&id, &kind, &channel, &status, &created, &delivered, &failure); err != nil {
			return err
		}
		out = append(out, map[string]any{"id": id, "type": kind, "channel": channel, "status": status, "createdAt": created, "deliveredAt": nullTimeString(delivered), "reason": failure.String})
	}
	write(w, 200, out)
	return rows.Err()
}
