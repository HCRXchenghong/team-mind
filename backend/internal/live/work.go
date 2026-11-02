package live

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
)

type project struct {
	ID             string    `json:"id"`
	TeamID         string    `json:"teamId"`
	DepartmentID   string    `json:"departmentId"`
	DepartmentName string    `json:"departmentName"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Status         string    `json:"status"`
	StartDate      *string   `json:"startDate"`
	DueDate        *string   `json:"dueDate"`
	Progress       int       `json:"progress"`
	Version        int       `json:"version"`
	CreatedAt      time.Time `json:"createdAt"`
}
type task struct {
	ID             string    `json:"id"`
	TeamID         string    `json:"teamId"`
	ProjectID      string    `json:"projectId"`
	DepartmentID   string    `json:"departmentId"`
	DepartmentName string    `json:"departmentName"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	AssigneeID     *string   `json:"assigneeId"`
	AssigneeName   *string   `json:"assigneeName"`
	Status         string    `json:"status"`
	Priority       string    `json:"priority"`
	StartDate      *string   `json:"startDate"`
	DueDate        *string   `json:"dueDate"`
	Progress       int       `json:"progress"`
	Milestone      bool      `json:"milestone"`
	Version        int       `json:"version"`
	CreatedBy      string    `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
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
	e := rows.Scan(&x.ID, &x.TeamID, &x.Name, &x.Description, &x.Status, &start, &due, &created, &progress, &ignored, &x.Version, &x.DepartmentID, &x.DepartmentName)
	x.StartDate = date(start)
	x.DueDate = date(due)
	x.CreatedAt = created
	x.Progress = int(progress.Int64)
	return x, e
}

const projectSelect = `SELECT HEX(p.id),HEX(p.team_id),p.name,p.description,p.status,p.start_date,p.due_date,p.created_at,(SELECT ROUND(AVG(t.progress)) FROM tasks t WHERE t.project_id=p.id AND t.deleted_at IS NULL) AS progress,HEX(p.created_by),p.version,COALESCE(HEX(p.department_id),''),COALESCE(d.name,'') FROM projects p LEFT JOIN departments d ON d.id=p.department_id`

func (s *Server) projects(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {
	if len(p) == 3 && r.Method == "GET" {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		status := r.URL.Query().Get("status")
		departmentID := strings.TrimSpace(r.URL.Query().Get("departmentId"))
		if len([]rune(q)) > 200 || status != "" && !in(status, "planning", "active", "paused", "completed", "archived") || departmentID != "" && !validID(departmentID) {
			return bad(400, "VALIDATION_ERROR", "项目筛选条件无效")
		}
		if unit, e := s.dataUnitForUser(r.Context(), u.ID, tid); e != nil {
			return e
		} else if unit != "" {
			departmentID = unit
		}
		n, size := page(r)
		filter := ` WHERE p.team_id=UNHEX(?) AND p.deleted_at IS NULL AND (?='' OR p.name LIKE CONCAT('%',?,'%')) AND (?='' OR p.status=?) AND (?='' OR HEX(p.department_id)=?)`
		args := []any{tid, q, q, status, status, strings.ToUpper(departmentID), strings.ToUpper(departmentID)}
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
		var x struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			Status       string `json:"status"`
			StartDate    string `json:"startDate"`
			DueDate      string `json:"dueDate"`
			DepartmentID string `json:"departmentId"`
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
		if _, e = activeTeamRoleTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		var departmentArg any
		if x.DepartmentID != "" {
			if kind, kindErr := s.teamKind(r.Context(), tid); kindErr != nil {
				return kindErr
			} else if kind != "company" {
				return bad(400, "ORG_MODE_TEAM", "团队版不启用部门")
			}
			if !validID(x.DepartmentID) {
				return bad(400, "VALIDATION_ERROR", "部门无效")
			}
			var departmentCount int
			if e = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM departments WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, x.DepartmentID, tid).Scan(&departmentCount); e != nil {
				return e
			}
			if departmentCount == 0 {
				return bad(400, "VALIDATION_ERROR", "部门不存在")
			}
			departmentArg = x.DepartmentID
		} else {
			var creatorDepartment sql.NullString
			if e = tx.QueryRowContext(r.Context(), `SELECT HEX(department_id) FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, tid, u.ID).Scan(&creatorDepartment); e == nil && creatorDepartment.Valid {
				departmentArg = creatorDepartment.String
			}
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO projects(id,team_id,department_id,name,description,status,start_date,due_date,created_by) VALUES(UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),?,?,?,?,?,UNHEX(?))`, pid, tid, departmentArg, departmentArg, x.Name, x.Description, x.Status, nullableDate(x.StartDate), nullableDate(x.DueDate), u.ID)
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
			if allowed, e := s.unitAccessToProject(r.Context(), u.ID, tid, pid); e != nil {
				return e
			} else if !allowed {
				return bad(403, "PROJECT_ACCESS_DENIED", "当前组织权限仅允许访问本部门/小组项目")
			}
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
		if r.Method == "PATCH" {
			var x struct {
				Name         string `json:"name"`
				Description  string `json:"description"`
				Status       string `json:"status"`
				StartDate    string `json:"startDate"`
				DueDate      string `json:"dueDate"`
				DepartmentID string `json:"departmentId"`
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
			var exists, currentDepartment, departmentLead string
			if e = tx.QueryRowContext(r.Context(), `SELECT HEX(p.id),COALESCE(HEX(p.department_id),''),COALESCE(HEX(d.lead_user_id),'') FROM projects p LEFT JOIN departments d ON d.id=p.department_id WHERE p.id=UNHEX(?) AND p.team_id=UNHEX(?) AND p.deleted_at IS NULL FOR UPDATE`, pid, tid).Scan(&exists, &currentDepartment, &departmentLead); e == sql.ErrNoRows {
				return bad(404, "NOT_FOUND", "项目不存在")
			} else if e != nil {
				return e
			}
			isAdmin := role == "team_admin" || role == "team_owner"
			if !isAdmin && (departmentLead == "" || departmentLead != u.ID) {
				return bad(403, "PROJECT_ACCESS_DENIED", "只有公司管理员或项目所属部门负责人可以编辑项目")
			}
			if !isAdmin && x.DepartmentID != currentDepartment {
				return bad(403, "DEPARTMENT_ADMIN_REQUIRED", "部门负责人不能变更项目所属部门")
			}
			var departmentArg any
			if x.DepartmentID != "" {
				if !validID(x.DepartmentID) {
					return bad(400, "VALIDATION_ERROR", "部门无效")
				}
				var departmentCount int
				if e = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM departments WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, x.DepartmentID, tid).Scan(&departmentCount); e != nil {
					return e
				}
				if departmentCount == 0 {
					return bad(400, "VALIDATION_ERROR", "部门不存在")
				}
				departmentArg = x.DepartmentID
			}
			_, e = tx.ExecContext(r.Context(), `UPDATE projects SET name=?,description=?,status=?,start_date=?,due_date=?,department_id=IF(? IS NULL,NULL,UNHEX(?)),version=version+1 WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, strings.TrimSpace(x.Name), x.Description, x.Status, nullableDate(x.StartDate), nullableDate(x.DueDate), departmentArg, departmentArg, pid, tid)
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
			if e := s.requireTeamAdmin(r, u, tid); e != nil {
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

const taskSelect = `SELECT HEX(t.id),HEX(t.team_id),HEX(t.project_id),COALESCE(HEX(p.department_id),''),COALESCE(d.name,''),t.title,t.description,HEX(t.assignee_id),u.display_name,t.status,t.priority,t.start_date,t.due_date,t.progress,t.is_milestone,t.version,HEX(t.created_by),t.created_at FROM tasks t LEFT JOIN users u ON u.id=t.assignee_id LEFT JOIN projects p ON p.id=t.project_id LEFT JOIN departments d ON d.id=p.department_id`

func scanTask(rows *sql.Rows) (task, error) {
	var x task
	var assignee, name sql.NullString
	var start, due sql.NullTime
	e := rows.Scan(&x.ID, &x.TeamID, &x.ProjectID, &x.DepartmentID, &x.DepartmentName, &x.Title, &x.Description, &assignee, &name, &x.Status, &x.Priority, &start, &due, &x.Progress, &x.Milestone, &x.Version, &x.CreatedBy, &x.CreatedAt)
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
		departmentID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("departmentId")))
		assigneeID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("assigneeId")))
		dateFrom := r.URL.Query().Get("dateFrom")
		dateTo := r.URL.Query().Get("dateTo")
		if projectID != "" && !validID(projectID) {
			return bad(400, "INVALID_ID", "项目 ID 无效")
		}
		if departmentID != "" && !validID(departmentID) {
			return bad(400, "INVALID_ID", "部门 ID 无效")
		}
		if status != "" && !in(status, "todo", "doing", "blocked", "review", "done", "cancelled") {
			return bad(400, "VALIDATION_ERROR", "任务状态筛选无效")
		}
		if assigneeID != "" && assigneeID != "unassigned" && !validID(assigneeID) {
			return bad(400, "INVALID_ID", "负责人筛选无效")
		}
		if r.URL.Query().Get("gantt") != "1" {
			if unit, e := s.dataUnitForUser(r.Context(), u.ID, tid); e != nil {
				return e
			} else if unit != "" {
				departmentID = unit
			}
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
		filter := ` WHERE t.team_id=UNHEX(?) AND t.deleted_at IS NULL AND (?='' OR t.title LIKE CONCAT('%',?,'%')) AND (?='' OR t.status=?) AND (?='' OR HEX(t.project_id)=?) AND (?='' OR HEX(p.department_id)=?) AND (?='' OR (?='unassigned' AND t.assignee_id IS NULL) OR HEX(t.assignee_id)=?) AND (?='' OR (t.due_date IS NOT NULL AND t.due_date>=?)) AND (?='' OR (t.start_date IS NOT NULL AND t.start_date<=?))`
		args := []any{tid, q, q, status, status, projectID, strings.ToUpper(projectID), departmentID, departmentID, assigneeID, assigneeID, assigneeID, dateFrom, dateFrom, dateTo, dateTo}
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
		e = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM tasks t LEFT JOIN projects p ON p.id=t.project_id`+filter, args...).Scan(&count)
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
			return bad(403, "TEAM_ROLE_CHANGED", "你的公司权限已变化，请刷新后重试")
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
		if allowed, accessErr := s.unitAccessToProject(r.Context(), u.ID, tid, x.ProjectID); accessErr != nil {
			return accessErr
		} else if !allowed {
			return bad(403, "PROJECT_ACCESS_DENIED", "当前组织权限仅允许在本部门/小组项目创建任务")
		}
		var exists int
		if e = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, tid, x.AssigneeID).Scan(&exists); e != nil {
			return e
		}
		if exists == 0 {
			return bad(400, "ASSIGNEE_NOT_MEMBER", "负责人不是公司成员")
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
	if allowed, e := s.unitAccessToTask(r.Context(), u.ID, tid, taskID); e != nil {
		return e
	} else if !allowed {
		return bad(403, "TASK_ACCESS_DENIED", "当前组织权限仅允许访问本部门/小组任务")
	}
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
			Status      *string `json:"status"`
			Progress    *int    `json:"progress"`
			Title       *string `json:"title"`
			Description *string `json:"description"`
			Priority    *string `json:"priority"`
			AssigneeID  *string `json:"assigneeId"`
			StartDate   *string `json:"startDate"`
			DueDate     *string `json:"dueDate"`
			Milestone   *bool   `json:"milestone"`
			Version     int     `json:"version"`
		}
		if e = body(w, r, &input); e != nil {
			return e
		}
		hasPlanFields := input.Title != nil || input.Description != nil || input.Priority != nil || input.AssigneeID != nil || input.StartDate != nil || input.DueDate != nil || input.Milestone != nil
		if input.Version < 1 || input.Status == nil && input.Progress == nil && !hasPlanFields {
			return bad(400, "VALIDATION_ERROR", "请提供任务状态、进度或计划字段")
		}
		if input.Status != nil && !in(*input.Status, "todo", "doing", "blocked", "review", "done", "cancelled") {
			return bad(400, "VALIDATION_ERROR", "任务状态无效")
		}
		if input.Progress != nil && (*input.Progress < 0 || *input.Progress > 100) {
			return bad(400, "VALIDATION_ERROR", "任务进度需在 0–100 之间")
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
		actingRole, e := activeTeamRoleTx(r.Context(), tx, tid, u)
		if e != nil {
			return e
		}
		// Plan fields stay behind approval for ordinary members. Administrators
		// can edit them directly; both paths are audited and versioned.
		if hasPlanFields && actingRole != "team_admin" && actingRole != "team_owner" {
			return bad(403, "TASK_PLAN_APPROVAL_REQUIRED", "标题、负责人和排期等计划字段需提交审批；管理员可直接编辑")
		}
		if !hasPlanFields {
			canContribute, _, accessErr := s.authorize(r.Context(), u.ID, authzContribute, "task", taskID)
			if accessErr != nil {
				return accessErr
			}
			if !canContribute {
				return bad(403, "TASK_ACCESS_DENIED", "只能更新自己创建、负责或参与的任务")
			}
		}
		if input.Version != current.Version {
			return bad(409, "VERSION_CONFLICT", "任务已被其他人更新，请刷新后重试")
		}
		snap := applySnapshotPatch(snapshotForTask(current), input.Title, input.Description, input.Priority, input.Status, input.AssigneeID, input.StartDate, input.DueDate, input.Progress, input.Milestone)
		snap.Title = strings.TrimSpace(snap.Title)
		if hasPlanFields {
			if e = s.validateSnapshot(r.Context(), tx, tid, snap); e != nil {
				return e
			}
		}
		assigneeID := any(nil)
		if snap.AssigneeID != nil {
			assigneeID = *snap.AssigneeID
		}
		res, e := tx.ExecContext(r.Context(), `UPDATE tasks SET title=?,description=?,priority=?,status=?,progress=?,assignee_id=UNHEX(?),start_date=?,due_date=?,is_milestone=?,version=version+1 WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND version=? AND deleted_at IS NULL`, snap.Title, snap.Description, snap.Priority, snap.Status, snap.Progress, assigneeID, nullableDate(ptrString(snap.StartDate)), nullableDate(ptrString(snap.DueDate)), snap.Milestone, taskID, tid, input.Version)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(409, "VERSION_CONFLICT", "任务已被其他人更新，请刷新后重试")
		}
		if hasPlanFields {
			_, e = tx.ExecContext(r.Context(), `INSERT INTO task_plan_versions(team_id,task_id,version,snapshot,change_source,actor_id) VALUES(UNHEX(?),UNHEX(?),?,?,'manual',UNHEX(?))`, tid, taskID, input.Version+1, marshalSnapshot(snap), u.ID)
		} else {
			_, e = tx.ExecContext(r.Context(), `INSERT INTO task_plan_versions(team_id,task_id,version,snapshot,change_source,actor_id) VALUES(UNHEX(?),UNHEX(?),?,JSON_OBJECT('status',?,'progress',?),'progress_update',UNHEX(?))`, tid, taskID, input.Version+1, snap.Status, snap.Progress, u.ID)
		}
		if e != nil {
			return e
		}
		action := "task.progress.update"
		if hasPlanFields {
			action = "task.update"
		}
		if e = addAudit(tx, u.ID, tid, action, "task", taskID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]int{"version": input.Version + 1})
		return nil
	}
	if len(p) == 4 && r.Method == "DELETE" {
		if e := s.requireTeamAdmin(r, u, tid); e != nil {
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
