package live

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const ganttShareAll = "all"

type ganttShareView struct {
	Enabled   bool       `json:"enabled"`
	Scope     string     `json:"scope"`
	Token     string     `json:"token,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

func ganttShareSecret() []byte {
	raw := strings.TrimSpace(os.Getenv("GANTT_SHARE_SECRET"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("LOGIN_RATE_LIMIT_HMAC_KEY"))
	}
	if raw == "" {
		raw = "teammind-local-development-only-gantt-share-secret"
	}
	sum := sha256.Sum256([]byte("teammind-gantt-share-v1:" + raw))
	return sum[:]
}

func ganttShareSignature(teamID, scope string, version int) string {
	mac := hmac.New(sha256.New, ganttShareSecret())
	_, _ = mac.Write([]byte(fmt.Sprintf("%s.%s.%d", strings.ToLower(teamID), scope, version)))
	return hex.EncodeToString(mac.Sum(nil))
}

func ganttShareToken(teamID, scope string, version int) string {
	payload := fmt.Sprintf("%s.%s.%d.%s", strings.ToLower(teamID), scope, version, ganttShareSignature(teamID, scope, version))
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func parseGanttShareToken(value string) (string, string, int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", "", 0, errors.New("invalid share token")
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 4 {
		return "", "", 0, errors.New("invalid share token")
	}
	teamID := strings.ToLower(parts[0])
	scope := parts[1]
	validScope := scope == ganttShareAll || validID(scope)
	if strings.HasPrefix(scope, "unit:") && validID(strings.TrimPrefix(scope, "unit:")) {
		validScope = true
	}
	if !validID(teamID) || !validScope {
		return "", "", 0, errors.New("invalid share token")
	}
	version, err := strconv.Atoi(parts[2])
	if err != nil || version < 1 || version > 1_000_000_000 {
		return "", "", 0, errors.New("invalid share token")
	}
	expected := ganttShareSignature(teamID, scope, version)
	if !hmac.Equal([]byte(expected), []byte(parts[3])) {
		return "", "", 0, errors.New("invalid share token")
	}
	return teamID, scope, version, nil
}

func normalizeGanttScope(projectID, unitID string) (string, error) {
	projectID = strings.TrimSpace(strings.ToLower(projectID))
	unitID = strings.TrimSpace(strings.ToLower(unitID))
	if projectID != "" && unitID != "" {
		return "", bad(http.StatusBadRequest, "VALIDATION_ERROR", "项目和部门/小组不能同时指定")
	}
	if unitID != "" {
		if !validID(unitID) {
			return "", bad(http.StatusBadRequest, "VALIDATION_ERROR", "部门/小组 ID 无效")
		}
		return "unit:" + unitID, nil
	}
	if projectID == "" {
		return ganttShareAll, nil
	}
	if !validID(projectID) {
		return "", bad(http.StatusBadRequest, "VALIDATION_ERROR", "项目 ID 无效")
	}
	return projectID, nil
}

func (s *Server) ganttScopeBelongsToTeam(r *http.Request, teamID, scope string) error {
	if scope == ganttShareAll {
		return nil
	}
	if strings.HasPrefix(scope, "unit:") {
		var exists int
		err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM departments WHERE team_id=UNHEX(?) AND id=UNHEX(?) AND deleted_at IS NULL`, teamID, strings.TrimPrefix(scope, "unit:")).Scan(&exists)
		if err != nil {
			return err
		}
		if exists == 0 {
			return bad(http.StatusNotFound, "NOT_FOUND", "部门/小组不存在")
		}
		return nil
	}
	var exists int
	err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM projects WHERE team_id=UNHEX(?) AND id=UNHEX(?) AND deleted_at IS NULL`, teamID, scope).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		return bad(http.StatusNotFound, "NOT_FOUND", "项目不存在")
	}
	return nil
}

func (s *Server) ganttShareStatus(r *http.Request, teamID, scope string, includeToken bool) (ganttShareView, error) {
	view := ganttShareView{Scope: scope}
	var version int
	var updated time.Time
	err := s.DB.QueryRowContext(r.Context(), `SELECT enabled,token_version,updated_at FROM gantt_public_shares WHERE team_id=UNHEX(?) AND scope=?`, teamID, scope).Scan(&view.Enabled, &version, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return view, nil
	}
	if err != nil {
		return view, err
	}
	view.UpdatedAt = &updated
	if view.Enabled && includeToken {
		view.Token = ganttShareToken(teamID, scope, version)
	}
	return view, nil
}

// ganttShare is mounted below /teams/{teamId}/gantt-share. Every member can
// see whether the current view is public, but only team administrators can
// enable, disable, or rotate the signed link.
func (s *Server) ganttShare(w http.ResponseWriter, r *http.Request, u User, tid, role string) error {
	scopeRaw := r.URL.Query().Get("projectId")
	unitRaw := r.URL.Query().Get("unitId")
	var input struct {
		Enabled   *bool  `json:"enabled"`
		ProjectID string `json:"projectId"`
		UnitID    string `json:"unitId"`
		Rotate    bool   `json:"rotate"`
	}
	if r.Method == http.MethodPatch {
		if e := body(w, r, &input); e != nil {
			return e
		}
		scopeRaw = input.ProjectID
		unitRaw = input.UnitID
	}
	scope, err := normalizeGanttScope(scopeRaw, unitRaw)
	if err != nil {
		return err
	}
	if err = s.ganttScopeBelongsToTeam(r, tid, scope); err != nil {
		return err
	}
	isAdmin := admin(role) == nil
	if r.Method == http.MethodGet {
		view, err := s.ganttShareStatus(r, tid, scope, isAdmin)
		if err != nil {
			return err
		}
		write(w, http.StatusOK, view)
		return nil
	}
	if r.Method != http.MethodPatch {
		return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
	}
	if !isAdmin {
		return bad(http.StatusForbidden, "FORBIDDEN", "只有团队管理员可以管理公开链接")
	}
	if !input.Rotate && input.Enabled == nil {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请提供 enabled 或 rotate")
	}
	enabled := false
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.Rotate {
		enabled = true
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if input.Rotate {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO gantt_public_shares(team_id,scope,enabled,token_version,updated_by) VALUES(UNHEX(?),?,TRUE,2,UNHEX(?)) ON DUPLICATE KEY UPDATE enabled=TRUE,token_version=token_version+1,updated_by=VALUES(updated_by)`, tid, scope, u.ID)
	} else {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO gantt_public_shares(team_id,scope,enabled,token_version,updated_by) VALUES(UNHEX(?),?,?,1,UNHEX(?)) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),token_version=token_version+IF(VALUES(enabled),1,0),updated_by=VALUES(updated_by)`, tid, scope, enabled, u.ID)
	}
	if err != nil {
		return err
	}
	action := "team.gantt_share_disabled"
	if enabled {
		action = "team.gantt_share_enabled"
	}
	if input.Rotate {
		action = "team.gantt_share_rotated"
	}
	if err = addAudit(tx, u.ID, tid, action, "gantt_share", tid); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	view, err := s.ganttShareStatus(r, tid, scope, true)
	if err != nil {
		return err
	}
	write(w, http.StatusOK, view)
	return nil
}

// publicGantt serves the signed, read-only project timeline. It deliberately
// omits descriptions, emails, comments, and internal identifiers that are not
// required to render a Gantt chart.
func (s *Server) publicGantt(w http.ResponseWriter, r *http.Request, shareToken string) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	teamID, scope, version, err := parseGanttShareToken(shareToken)
	if err != nil {
		return bad(http.StatusNotFound, "SHARE_NOT_FOUND", "公开链接无效或已被关闭")
	}
	var teamName, teamDescription, teamTimezone, teamStatus string
	var shareEnabled bool
	err = s.DB.QueryRowContext(r.Context(), `SELECT t.name,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(t.settings,'$.description')),''),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(t.settings,'$.timezone')),'Asia/Shanghai'),t.status,s.enabled FROM gantt_public_shares s JOIN teams t ON t.id=s.team_id WHERE s.team_id=UNHEX(?) AND s.scope=? AND s.token_version=? AND t.deleted_at IS NULL`, teamID, scope, version).Scan(&teamName, &teamDescription, &teamTimezone, &teamStatus, &shareEnabled)
	if errors.Is(err, sql.ErrNoRows) || !shareEnabled || teamStatus != "active" {
		return bad(http.StatusNotFound, "SHARE_NOT_FOUND", "公开链接无效或已被关闭")
	}
	if err != nil {
		return err
	}
	unitID := ""
	if strings.HasPrefix(scope, "unit:") {
		unitID = strings.TrimPrefix(scope, "unit:")
	}

	projects := make([]map[string]any, 0)
	rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(id),name,status FROM projects WHERE team_id=UNHEX(?) AND deleted_at IS NULL AND (?=? OR HEX(id)=? OR (?<>'' AND HEX(department_id)=?)) ORDER BY created_at`, teamID, scope, ganttShareAll, scope, unitID, unitID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, name, status string
		if err = rows.Scan(&id, &name, &status); err != nil {
			rows.Close()
			return err
		}
		projects = append(projects, map[string]any{"id": id, "name": name, "status": status})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	tasks := make([]map[string]any, 0)
	rows, err = s.DB.QueryContext(r.Context(), `SELECT HEX(t.id),HEX(t.project_id),t.title,t.status,t.priority,t.start_date,t.due_date,t.progress,t.is_milestone,COALESCE(u.display_name,'') FROM tasks t LEFT JOIN users u ON u.id=t.assignee_id WHERE t.team_id=UNHEX(?) AND t.deleted_at IS NULL AND (?=? OR HEX(t.project_id)=? OR (?<>'' AND t.project_id IN (SELECT id FROM projects WHERE team_id=UNHEX(?) AND department_id=UNHEX(?) AND deleted_at IS NULL))) ORDER BY t.start_date IS NULL,t.start_date,t.due_date IS NULL,t.due_date,t.created_at`, teamID, scope, ganttShareAll, scope, unitID, teamID, unitID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, projectID, title, status, priority, assigneeName string
		var start, due sql.NullTime
		var progress int
		var milestone bool
		if err = rows.Scan(&id, &projectID, &title, &status, &priority, &start, &due, &progress, &milestone, &assigneeName); err != nil {
			rows.Close()
			return err
		}
		tasks = append(tasks, map[string]any{
			"id": id, "projectId": projectID, "title": title, "status": status,
			"priority": priority, "startDate": date(start), "dueDate": date(due),
			"progress": progress, "milestone": milestone, "assigneeName": assigneeName,
		})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	dependencies := make(map[string][]string)
	rows, err = s.DB.QueryContext(r.Context(), `SELECT HEX(d.task_id),HEX(d.depends_on_task_id) FROM task_dependencies d JOIN tasks t ON t.id=d.task_id WHERE d.team_id=UNHEX(?) AND t.deleted_at IS NULL AND (?=? OR HEX(t.project_id)=? OR (?<>'' AND t.project_id IN (SELECT id FROM projects WHERE team_id=UNHEX(?) AND department_id=UNHEX(?) AND deleted_at IS NULL))) ORDER BY d.task_id,d.depends_on_task_id`, teamID, scope, ganttShareAll, scope, unitID, teamID, unitID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var taskID, dependencyID string
		if err = rows.Scan(&taskID, &dependencyID); err != nil {
			rows.Close()
			return err
		}
		dependencies[taskID] = append(dependencies[taskID], dependencyID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	write(w, http.StatusOK, map[string]any{
		"team":         map[string]any{"name": teamName, "description": teamDescription, "timezone": teamTimezone},
		"scope":        scope,
		"projects":     projects,
		"tasks":        tasks,
		"dependencies": dependencies,
		"updatedAt":    time.Now().UTC(),
	})
	return nil
}
