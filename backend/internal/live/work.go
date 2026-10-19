package live

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
)

type project struct {
	ID          string    `json:"id"`
	TeamID      string    `json:"teamId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	StartDate   *string   `json:"startDate"`
	DueDate     *string   `json:"dueDate"`
	Progress    int       `json:"progress"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
}
type task struct {
	ID           string    `json:"id"`
	TeamID       string    `json:"teamId"`
	ProjectID    string    `json:"projectId"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	AssigneeID   *string   `json:"assigneeId"`
	AssigneeName *string   `json:"assigneeName"`
	Status       string    `json:"status"`
	Priority     string    `json:"priority"`
	StartDate    *string   `json:"startDate"`
	DueDate      *string   `json:"dueDate"`
	Progress     int       `json:"progress"`
	Milestone    bool      `json:"milestone"`
	Version      int       `json:"version"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`
}

func date(p sql.NullTime) *string {
	if !p.Valid {
		return nil
	}
	s := p.Time.Format("2006-01-02")
	return &s
}
func validDate(s string) bool {
	if s == "" {
		return true
	}
	t, e := time.Parse("2006-01-02", s)
	return e == nil && t.Format("2006-01-02") == s
}
func nullableDate(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func scanProject(rows *sql.Rows) (project, error) {
	var x project
	var start, due sql.NullTime
	var ignored string
	var created time.Time
	var progress sql.NullInt64
	e := rows.Scan(&x.ID, &x.TeamID, &x.Name, &x.Description, &x.Status, &start, &due, &created, &progress, &ignored, &x.Version)
	x.StartDate = date(start)
	x.DueDate = date(due)
	x.CreatedAt = created
	x.Progress = int(progress.Int64)
	return x, e
}

const projectSelect = `SELECT HEX(p.id),HEX(p.team_id),p.name,p.description,p.status,p.start_date,p.due_date,p.created_at,(SELECT ROUND(AVG(t.progress)) FROM tasks t WHERE t.project_id=p.id AND t.deleted_at IS NULL) AS progress,HEX(p.created_by),p.version FROM projects p`

func (s *Server) projects(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {
	if len(p) == 3 && r.Method == "GET" {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		status := r.URL.Query().Get("status")
		if len([]rune(q)) > 200 || status != "" && !in(status, "planning", "active", "paused", "completed", "archived") {
			return bad(400, "VALIDATION_ERROR", "项目筛选条件无效")
		}
		n, size := page(r)
		filter := ` WHERE p.team_id=UNHEX(?) AND p.deleted_at IS NULL AND (?='' OR p.name LIKE CONCAT('%',?,'%')) AND (?='' OR p.status=?)`
		args := []any{tid, q, q, status, status}
		rows, e := s.DB.QueryContext(r.Context(), projectSelect+filter+` ORDER BY p.created_at DESC LIMIT ? OFFSET ?`, append(args, size, (n-1)*size)...)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []project{}
		for rows.Next() {
			x, e := scanProject(rows)
			if e != nil {
				return e
			}
			out = append(out, x)
		}
		var count int
		e = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM projects p`+filter, args...).Scan(&count)
		if e != nil {
			return e
		}
		write(w, 200, map[string]any{"items": out, "total": count, "page": n, "pageSize": size})
		return rows.Err()
	}
	if len(p) == 3 && r.Method == "POST" {
		if e := admin(role); e != nil {
			return e
		}
		var x struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Status      string `json:"status"`
			StartDate   string `json:"startDate"`
			DueDate     string `json:"dueDate"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		x.Name = strings.TrimSpace(x.Name)
		if x.Status == "" {
			x.Status = "planning"
		}
		if x.Name == "" || len([]rune(x.Name)) > 200 || len(x.Description) > 10000 || !validDate(x.StartDate) || !validDate(x.DueDate) || x.StartDate != "" && x.DueDate != "" && x.DueDate < x.StartDate || !in(x.Status, "planning", "active", "paused", "completed", "archived") {
			return bad(400, "VALIDATION_ERROR", "项目参数无效")
		}
		pid := id()
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO projects(id,team_id,name,description,status,start_date,due_date,created_by) VALUES(UNHEX(?),UNHEX(?),?,?,?,?,?,UNHEX(?))`, pid, tid, x.Name, x.Description, x.Status, nullableDate(x.StartDate), nullableDate(x.DueDate), u.ID)
		if e != nil {
			return e
		}
		if e = addAudit(tx, u.ID, tid, "project.create", "project", pid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 201, map[string]string{"id": pid})
		return nil
	}
	if len(p) == 4 {
		pid := p[3]
		if !validID(pid) {
			return bad(404, "NOT_FOUND", "项目不存在")
		}
		if r.Method == "GET" {
			rows, e := s.DB.QueryContext(r.Context(), projectSelect+` WHERE p.id=UNHEX(?) AND p.team_id=UNHEX(?) AND p.deleted_at IS NULL`, pid, tid)
			if e != nil {
				return e
			}
			defer rows.Close()
			if !rows.Next() {
				return bad(404, "NOT_FOUND", "项目不存在")
			}
			x, e := scanProject(rows)
			if e != nil {
				return e
			}
			write(w, 200, x)
			return nil
		}
		if e := admin(role); e != nil {
			return e
		}
		if r.Method == "PATCH" {
			var x struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Status      string `json:"status"`
				StartDate   string `json:"startDate"`
				DueDate     string `json:"dueDate"`
			}
			if e := body(w, r, &x); e != nil {
				return e
			}
			if strings.TrimSpace(x.Name) == "" || len([]rune(x.Name)) > 200 || len(x.Description) > 10000 || !in(x.Status, "planning", "active", "paused", "completed", "archived") || !validDate(x.StartDate) || !validDate(x.DueDate) || x.StartDate != "" && x.DueDate != "" && x.DueDate < x.StartDate {
				return bad(400, "VALIDATION_ERROR", "项目参数无效")
			}
			tx, e := s.DB.BeginTx(r.Context(), nil)
			if e != nil {
				return e
			}
			defer tx.Rollback()
			if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
				return e
			}
			var exists string
			if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM projects WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL FOR UPDATE`, pid, tid).Scan(&exists); e == sql.ErrNoRows {
				return bad(404, "NOT_FOUND", "项目不存在")
			} else if e != nil {
				return e
			}
			_, e = tx.ExecContext(r.Context(), `UPDATE projects SET name=?,description=?,status=?,start_date=?,due_date=?,version=version+1 WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, strings.TrimSpace(x.Name), x.Description, x.Status, nullableDate(x.StartDate), nullableDate(x.DueDate), pid, tid)
			if e != nil {
				return e
			}
			if e = addAudit(tx, u.ID, tid, "project.update", "project", pid); e != nil {
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			write(w, 200, map[string]bool{"saved": true})
			return nil
		}
		if r.Method == "DELETE" {
			tx, e := s.DB.BeginTx(r.Context(), nil)
			if e != nil {
				return e
			}
			defer tx.Rollback()
			if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
				return e
			}
			var exists string
			if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM projects WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL FOR UPDATE`, pid, tid).Scan(&exists); e == sql.ErrNoRows {
				return bad(404, "NOT_FOUND", "项目不存在")
			} else if e != nil {
				return e
			}
			var count int
			if e = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM tasks WHERE project_id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, pid, tid).Scan(&count); e != nil {
				return e
			}
			if count > 0 {
				return bad(409, "PROJECT_NOT_EMPTY", "请先处理项目中的任务")
			}
			res, e := tx.ExecContext(r.Context(), `UPDATE projects SET deleted_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, pid, tid)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				return bad(404, "NOT_FOUND", "项目不存在")
			}
			if e = addAudit(tx, u.ID, tid, "project.delete", "project", pid); e != nil {
				return e
			}
			if e = tx.Commit(); e != nil {
				return e
			}
			write(w, 200, map[string]bool{"deleted": true})
			return nil
		}
	}
	return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}
func in(value string, allowed ...string) bool {
	for _, v := range allowed {
		if v == value {
			return true
		}
	}
	return false
}

const taskSelect = `SELECT HEX(t.id),HEX(t.team_id),HEX(t.project_id),t.title,t.description,HEX(t.assignee_id),u.display_name,t.status,t.priority,t.start_date,t.due_date,t.progress,t.is_milestone,t.version,HEX(t.created_by),t.created_at FROM tasks t LEFT JOIN users u ON u.id=t.assignee_id`

func scanTask(rows *sql.Rows) (task, error) {
	var x task
	var assignee, name sql.NullString
	var start, due sql.NullTime
	e := rows.Scan(&x.ID, &x.TeamID, &x.ProjectID, &x.Title, &x.Description, &assignee, &name, &x.Status, &x.Priority, &start, &due, &x.Progress, &x.Milestone, &x.Version, &x.CreatedBy, &x.CreatedAt)
	if assignee.Valid {
		x.AssigneeID = &assignee.String
	}
	if name.Valid {
		x.AssigneeName = &name.String
	}
	x.StartDate = date(start)
	x.DueDate = date(due)
	return x, e
}
func (s *Server) taskByID(r *http.Request, tid, taskID string) (task, error) {
	rows, e := s.DB.QueryContext(r.Context(), taskSelect+` WHERE t.id=UNHEX(?) AND t.team_id=UNHEX(?) AND t.deleted_at IS NULL`, taskID, tid)
	if e != nil {
		return task{}, e
	}
	defer rows.Close()
	if !rows.Next() {
		return task{}, bad(404, "NOT_FOUND", "任务不存在")
	}
	return scanTask(rows)
}
func (s *Server) tasks(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {
	if len(p) == 3 && r.Method == "GET" {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		status := r.URL.Query().Get("status")
		projectID := r.URL.Query().Get("projectId")
		assigneeID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("assigneeId")))
		dateFrom := r.URL.Query().Get("dateFrom")
		dateTo := r.URL.Query().Get("dateTo")
		if projectID != "" && !validID(projectID) {
			return bad(400, "INVALID_ID", "项目 ID 无效")
		}
		if status != "" && !in(status, "todo", "doing", "blocked", "review", "done", "cancelled") {
			return bad(400, "VALIDATION_ERROR", "任务状态筛选无效")
		}
		if assigneeID != "" && assigneeID != "unassigned" && !validID(assigneeID) {
			return bad(400, "INVALID_ID", "负责人筛选无效")
		}
		if !validDate(dateFrom) || !validDate(dateTo) || dateFrom != "" && dateTo != "" && dateTo < dateFrom {
			return bad(400, "VALIDATION_ERROR", "日期筛选范围无效")
		}
		sortBy := r.URL.Query().Get("sortBy")
		sortColumn, ok := map[string]string{"createdAt": "t.created_at", "title": "t.title", "dueDate": "t.due_date", "priority": "FIELD(t.priority,'urgent','high','medium','low')", "assignee": "u.display_name"}[sortBy]
		if sortBy == "" {
			sortColumn = "t.created_at"
		} else if !ok {
			return bad(400, "VALIDATION_ERROR", "排序字段无效")
		}
		sortDir := strings.ToUpper(r.URL.Query().Get("sortDir"))
		if sortDir == "" {
			sortDir = "DESC"
		}
		if sortDir != "ASC" && sortDir != "DESC" {
			return bad(400, "VALIDATION_ERROR", "排序方向无效")
		}
		n, size := page(r)
		filter := ` WHERE t.team_id=UNHEX(?) AND t.deleted_at IS NULL AND (?='' OR t.title LIKE CONCAT('%',?,'%')) AND (?='' OR t.status=?) AND (?='' OR HEX(t.project_id)=?) AND (?='' OR (?='unassigned' AND t.assignee_id IS NULL) OR HEX(t.assignee_id)=?) AND (?='' OR (t.due_date IS NOT NULL AND t.due_date>=?)) AND (?='' OR (t.start_date IS NOT NULL AND t.start_date<=?))`
		args := []any{tid, q, q, status, status, projectID, strings.ToUpper(projectID), assigneeID, assigneeID, assigneeID, dateFrom, dateFrom, dateTo, dateTo}
		rows, e := s.DB.QueryContext(r.Context(), taskSelect+filter+` ORDER BY `+sortColumn+` `+sortDir+`,t.id ASC LIMIT ? OFFSET ?`, append(args, size, (n-1)*size)...)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []task{}
		for rows.Next() {
			x, e := scanTask(rows)
			if e != nil {
				return e
			}
			out = append(out, x)
		}
		var count int
		e = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM tasks t`+filter, args...).Scan(&count)
		if e != nil {
			return e
		}
		write(w, 200, map[string]any{"items": out, "total": count, "page": n, "pageSize": size})
		return rows.Err()
	}
	if len(p) == 3 && r.Method == "POST" {
		var x struct {
			ProjectID   string `json:"projectId"`
			Title       string `json:"title"`
			Description string `json:"description"`
			AssigneeID  string `json:"assigneeId"`
			Priority    string `json:"priority"`
			StartDate   string `json:"startDate"`
			DueDate     string `json:"dueDate"`
			Milestone   bool   `json:"milestone"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		x.Title = strings.TrimSpace(x.Title)
		if x.Priority == "" {
			x.Priority = "medium"
		}
		if !validID(x.ProjectID) || x.Title == "" || len([]rune(x.Title)) > 240 || len(x.Description) > 20000 || !in(x.Priority, "low", "medium", "high", "urgent") || !validDate(x.StartDate) || !validDate(x.DueDate) || x.StartDate != "" && x.DueDate != "" && x.DueDate < x.StartDate {
			return bad(400, "VALIDATION_ERROR", "任务参数无效")
		}
		if x.AssigneeID == "" {
			x.AssigneeID = u.ID
		}
		if !validID(x.AssigneeID) {
			return bad(400, "INVALID_ID", "负责人 ID 无效")
		}
		if role == "member" && x.AssigneeID != u.ID {
			return bad(403, "ASSIGN_DENIED", "成员只能创建自己的任务")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		actingRole, e := activeTeamRoleTx(r.Context(), tx, tid, u)
		if e != nil {
			return e
		}
		if actingRole != role {
			return bad(403, "TEAM_ROLE_CHANGED", "你的团队权限已变化，请刷新后重试")
		}
		if actingRole == "member" && x.AssigneeID != u.ID {
			return bad(403, "ASSIGN_DENIED", "成员只能创建自己的任务")
		}
		var projectID string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM projects WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL FOR UPDATE`, x.ProjectID, tid).Scan(&projectID); e == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "项目不存在")
		} else if e != nil {
			return e
		}
		var exists int
		if e = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, tid, x.AssigneeID).Scan(&exists); e != nil {
			return e
		}
		if exists == 0 {
			return bad(400, "ASSIGNEE_NOT_MEMBER", "负责人不是团队成员")
		}
		taskID := id()
		_, e = tx.ExecContext(r.Context(), `INSERT INTO tasks(id,team_id,project_id,title,description,created_by,assignee_id,priority,start_date,due_date,is_milestone) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),?,?,UNHEX(?),UNHEX(?),?,?,?,?)`, taskID, tid, x.ProjectID, x.Title, x.Description, u.ID, x.AssigneeID, x.Priority, nullableDate(x.StartDate), nullableDate(x.DueDate), x.Milestone)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO task_plan_versions(team_id,task_id,version,snapshot,change_source,actor_id) VALUES(UNHEX(?),UNHEX(?),1,JSON_OBJECT('title',?,'description',?,'priority',?,'assigneeId',?,'startDate',?,'dueDate',?,'milestone',?),'manual',UNHEX(?))`, tid, taskID, x.Title, x.Description, x.Priority, x.AssigneeID, nullableDate(x.StartDate), nullableDate(x.DueDate), x.Milestone, u.ID)
		if e != nil {
			return e
		}
		if e = addAudit(tx, u.ID, tid, "task.create", "task", taskID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 201, map[string]string{"id": taskID})
		return nil
	}
	if len(p) < 4 || !validID(p[3]) {
		return bad(404, "NOT_FOUND", "任务不存在")
	}
	taskID := p[3]
	if len(p) == 4 && r.Method == "GET" {
		x, e := s.taskByID(r, tid, taskID)
		if e != nil {
			return e
		}
		write(w, 200, x)
		return nil
	}
	if len(p) == 4 && r.Method == "PATCH" {
		_, e := s.taskByID(r, tid, taskID)
		if e != nil {
			return e
		}
		var input struct {
			Status   string `json:"status"`
			Progress int    `json:"progress"`
			Version  int    `json:"version"`
		}
		if e = body(w, r, &input); e != nil {
			return e
		}
		if !in(input.Status, "todo", "doing", "blocked", "review", "done", "cancelled") || input.Progress < 0 || input.Progress > 100 || input.Version < 1 {
			return bad(400, "VALIDATION_ERROR", "任务状态、进度或版本无效")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		current, e := taskForUpdate(r.Context(), tx, tid, taskID)
		if e == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "任务不存在")
		}
		if e != nil {
			return e
		}
		canContribute, accessErr := canContributeTaskTx(r.Context(), tx, tid, u, current)
		if accessErr != nil {
			return accessErr
		}
		if !canContribute {
			return bad(403, "TASK_ACCESS_DENIED", "只能更新自己创建、负责或参与的任务")
		}
		if input.Version != current.Version {
			return bad(409, "VERSION_CONFLICT", "任务已被其他人更新，请刷新后重试")
		}
		res, e := tx.ExecContext(r.Context(), `UPDATE tasks SET status=?,progress=?,version=version+1 WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND version=? AND deleted_at IS NULL`, input.Status, input.Progress, taskID, tid, input.Version)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(409, "VERSION_CONFLICT", "任务已被其他人更新，请刷新后重试")
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO task_plan_versions(team_id,task_id,version,snapshot,change_source,actor_id) VALUES(UNHEX(?),UNHEX(?),?,JSON_OBJECT('status',?,'progress',?),'progress_update',UNHEX(?))`, tid, taskID, input.Version+1, input.Status, input.Progress, u.ID)
		if e != nil {
			return e
		}
		if e = addAudit(tx, u.ID, tid, "task.progress.update", "task", taskID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]int{"version": input.Version + 1})
		return nil
	}
	if len(p) == 4 && r.Method == "DELETE" {
		if e := admin(role); e != nil {
			return e
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		res, e := tx.ExecContext(r.Context(), `UPDATE tasks SET deleted_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, taskID, tid)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(404, "NOT_FOUND", "任务不存在")
		}
		if e = addAudit(tx, u.ID, tid, "task.delete", "task", taskID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"deleted": true})
		return nil
	}
	if len(p) == 5 && p[4] == "comments" {
		return s.comments(w, r, u, tid, taskID)
	}
	if len(p) == 6 && p[4] == "comments" {
		return s.commentByID(w, r, u, tid, taskID, p[5])
	}
	if len(p) == 5 && p[4] == "dependencies" {
		return s.dependencies(w, r, u, tid, taskID)
	}
	if len(p) == 5 && p[4] == "collaborators" {
		return s.collaborators(w, r, u, tid, taskID, "")
	}
	if len(p) == 6 && p[4] == "collaborators" {
		return s.collaborators(w, r, u, tid, taskID, p[5])
	}
	if len(p) == 5 && p[4] == "history" {
		return s.history(w, r, tid, taskID)
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}
