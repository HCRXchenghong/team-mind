package live

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type team struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Timezone    string `json:"timezone"`
	Role        string `json:"role"`
	IsLead      bool   `json:"isLead"`
	Status      string `json:"status"`
	Members     int    `json:"members"`
}

func (s *Server) role(r *http.Request, u User, tid string) (string, error) {
	if !validID(tid) {
		return "", bad(404, "NOT_FOUND", "公司不存在")
	}
	var role string
	e := s.DB.QueryRowContext(r.Context(), `SELECT m.role FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=UNHEX(?) AND m.user_id=UNHEX(?) AND m.status='active' AND t.status='active'`, tid, u.ID).Scan(&role)
	if e == sql.ErrNoRows {
		return "", bad(403, "TEAM_ACCESS_DENIED", "无权访问该公司")
	}
	return role, e
}
func admin(role string) error {
	if role != "team_admin" && role != "team_owner" {
		return bad(403, "TEAM_ADMIN_REQUIRED", "需要公司管理员权限")
	}
	return nil
}
func owner(role string) error {
	if role != "team_owner" {
		return bad(403, "TEAM_OWNER_REQUIRED", "需要公司所有者权限")
	}
	return nil
}
func (s *Server) teams(w http.ResponseWriter, r *http.Request, u User, path string) error {
	if r.Method == "GET" {
		if path != "me/teams" {
			return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(t.id),t.name,t.kind,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(t.settings,'$.description')),''),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(t.settings,'$.timezone')),'Asia/Shanghai'),m.role,t.status,(SELECT COUNT(*) FROM team_memberships x WHERE x.team_id=t.id AND x.status='active'),EXISTS(SELECT 1 FROM departments d WHERE d.team_id=t.id AND d.lead_user_id=m.user_id AND d.deleted_at IS NULL) FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.user_id=UNHEX(?) AND m.status='active' AND t.status IN ('active','pending_review') ORDER BY t.created_at DESC`, u.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []team{}
		for rows.Next() {
			var t team
			if e = rows.Scan(&t.ID, &t.Name, &t.Kind, &t.Description, &t.Timezone, &t.Role, &t.Status, &t.Members, &t.IsLead); e != nil {
				return e
			}
			out = append(out, t)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		write(w, 200, out)
		return nil
	}
	if path != "teams" {
		return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
	}
	if e := method(r, "POST"); e != nil {
		return e
	}
	var x struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Timezone    string `json:"timezone"`
		Kind        string `json:"kind"`
	}
	if e := body(w, r, &x); e != nil {
		return e
	}
	x.Name = strings.TrimSpace(x.Name)
	if len([]rune(x.Name)) < 2 || len([]rune(x.Name)) > 160 || len(x.Description) > 2000 {
		return bad(400, "VALIDATION_ERROR", "团队名称需为 2–160 字，描述不超过 2000 字")
	}
	if x.Kind == "" {
		x.Kind = "company"
	}
	if x.Kind != "team" && x.Kind != "company" {
		return bad(400, "VALIDATION_ERROR", "组织类型必须为团队或公司")
	}
	if x.Timezone == "" {
		x.Timezone = "Asia/Shanghai"
	}
	if _, e := time.LoadLocation(x.Timezone); e != nil {
		return bad(400, "VALIDATION_ERROR", "时区无效")
	}
	if !u.SuperAdmin {
		var memberships int
		if e := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM team_memberships m JOIN teams t ON t.id=m.team_id
			WHERE m.user_id=UNHEX(?) AND m.status='active' AND t.deleted_at IS NULL AND t.status IN ('active','pending_review')`, u.ID).Scan(&memberships); e != nil {
			return e
		}
		if memberships > 0 {
			return bad(409, "SINGLE_COMPANY_ONLY", "当前账号已属于一个公司；单公司版不支持创建多个公司")
		}
	}
	tid := id()
	settings, _ := json.Marshal(map[string]string{"description": x.Description, "timezone": x.Timezone})
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	status := "active"
	if os.Getenv("TEAM_APPROVAL_REQUIRED") == "1" && !u.SuperAdmin {
		status = "pending_review"
	}
	_, e = tx.ExecContext(r.Context(), `INSERT INTO teams(id,name,kind,slug,status,created_by,settings) VALUES(UNHEX(?),?,?,?,?,UNHEX(?),?)`, tid, x.Name, x.Kind, "team-"+tid, status, u.ID, settings)
	if e != nil {
		return e
	}
	if x.Kind == "company" {
		departmentID := id()
		if _, e = tx.ExecContext(r.Context(), `INSERT INTO departments(id,team_id,name) VALUES(UNHEX(?),UNHEX(?),'默认部门')`, departmentID, tid); e != nil {
			return e
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO team_memberships(team_id,user_id,role,status,department_id,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UNHEX(?),UTC_TIMESTAMP(3))`, tid, u.ID, departmentID)
	} else {
		_, e = tx.ExecContext(r.Context(), `INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UTC_TIMESTAMP(3))`, tid, u.ID)
	}
	if e != nil {
		return e
	}
	if e = addAudit(tx, u.ID, tid, "team.create", "team", tid); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	write(w, 201, team{ID: tid, Name: x.Name, Kind: x.Kind, Description: x.Description, Timezone: x.Timezone, Role: "team_owner", Status: status, Members: 1})
	return nil
}
func addAudit(tx *sql.Tx, actor, teamID, action, typ, rid string) error {
	return addAuditWithSummary(tx, actor, teamID, action, typ, rid, nil)
}

// addAuditWithSummary stores per-event context in audit_logs.after_summary so
// admin history can show the reason captured at decision time instead of the
// team's latest mutable review_reason.
func addAuditWithSummary(tx *sql.Tx, actor, teamID, action, typ, rid string, summary map[string]any) error {
	var teamArg any
	if teamID != "" {
		teamArg = teamID
	}
	var summaryArg any
	if summary != nil {
		encoded, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		summaryArg = encoded
	}
	_, e := tx.Exec(`INSERT INTO audit_logs(actor_user_id,team_id,action,resource_type,resource_id,after_summary) VALUES(UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),?,?,UNHEX(?),?)`, actor, teamArg, teamArg, action, typ, rid, summaryArg)
	return e
}
func (s *Server) teamRoute(w http.ResponseWriter, r *http.Request, u User, p []string) error {
	if len(p) < 2 {
		return bad(404, "NOT_FOUND", "资源不存在")
	}
	tid := p[1]
	role, e := s.role(r, u, tid)
	if e != nil {
		return e
	}
	if len(p) == 2 {
		if r.Method == "GET" {
			return s.teamDetail(w, r, u, tid, role)
		}
		if r.Method == "PATCH" {
			return s.teamSettings(w, r, u, tid, role)
		}
		return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
	}
	switch p[2] {
	case "permissions":
		return s.teamPermissions(w, r, u.ID, tid)
	case "summary":
		if e = method(r, "GET"); e != nil {
			return e
		}
		return s.summary(w, r, tid, role)
	case "departments":
		return s.departments(w, r, u, tid, role, p)
	case "members":
		return s.members(w, r, u, tid, role, p)
	case "invitations":
		return s.invitations(w, r, u, tid, role, p)
	case "projects":
		return s.projects(w, r, u, tid, role, p)
	case "gantt-share":
		return s.ganttShare(w, r, u, tid, role)
	case "gantt-insights":
		return s.ganttInsights(w, r, u, tid, role)
	case "tasks":
		return s.tasks(w, r, u, tid, role, p)
	case "plan-proposals":
		return s.proposals(w, r, u, tid, role, p)
	case "project-proposals":
		return s.projectProposals(w, r, u, tid, role, p)
	case "member-proposals":
		return s.memberProposals(w, r, u, tid, role, p)
	case "memories":
		return s.teamMemories(w, r, u, tid, role, p)
	case "kind":
		return s.convertTeamKind(w, r, u, tid, role)
	case "authz":
		return s.teamAuthz(w, r, u, tid)
	case "access-policy":
		return s.teamAccessPolicyRoute(w, r, u, tid, role)
	case "ai":
		return s.teamAIAdmin(w, r, u, tid, role, p[2:])
	case "audit-log":
		if e = s.requireTeamAdmin(r, u, tid); e != nil {
			return e
		}
		return s.auditLog(w, r, tid)
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}
func (s *Server) teamDetail(w http.ResponseWriter, r *http.Request, u User, tid, role string) error {
	var t team
	e := s.DB.QueryRowContext(r.Context(), `SELECT HEX(t.id),t.name,t.kind,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(t.settings,'$.description')),''),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(t.settings,'$.timezone')),'Asia/Shanghai'),t.status,(SELECT COUNT(*) FROM team_memberships m WHERE m.team_id=t.id AND m.status='active'),EXISTS(SELECT 1 FROM departments d WHERE d.team_id=t.id AND d.lead_user_id=UNHEX(?) AND d.deleted_at IS NULL) FROM teams t WHERE t.id=UNHEX(?)`, u.ID, tid).Scan(&t.ID, &t.Name, &t.Kind, &t.Description, &t.Timezone, &t.Status, &t.Members, &t.IsLead)
	if e != nil {
		return e
	}
	t.Role = role
	write(w, 200, t)
	return nil
}
func (s *Server) teamSettings(w http.ResponseWriter, r *http.Request, u User, tid, role string) error {
	if e := s.requireTeamAdmin(r, u, tid); e != nil {
		return e
	}
	var x struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Timezone    string `json:"timezone"`
	}
	if e := body(w, r, &x); e != nil {
		return e
	}
	x.Name = strings.TrimSpace(x.Name)
	if len([]rune(x.Name)) < 2 || len([]rune(x.Name)) > 160 || len(x.Description) > 2000 {
		return bad(400, "VALIDATION_ERROR", "团队名称或描述长度无效")
	}
	if _, e := time.LoadLocation(x.Timezone); e != nil {
		return bad(400, "VALIDATION_ERROR", "时区无效")
	}
	settings, _ := json.Marshal(map[string]string{"description": x.Description, "timezone": x.Timezone})
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
		return e
	}
	_, e = tx.ExecContext(r.Context(), `UPDATE teams SET name=?,settings=? WHERE id=UNHEX(?)`, x.Name, settings, tid)
	if e != nil {
		return e
	}
	if e = addAudit(tx, u.ID, tid, "team.update", "team", tid); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	write(w, 200, map[string]bool{"saved": true})
	return nil
}
func (s *Server) summary(w http.ResponseWriter, r *http.Request, tid, role string) error {
	var projects, tasks, blocked, pending, members, departments int
	e := s.DB.QueryRowContext(r.Context(), `SELECT (SELECT COUNT(*) FROM projects WHERE team_id=UNHEX(?) AND deleted_at IS NULL),(SELECT COUNT(*) FROM tasks WHERE team_id=UNHEX(?) AND deleted_at IS NULL),(SELECT COUNT(*) FROM tasks WHERE team_id=UNHEX(?) AND status='blocked' AND deleted_at IS NULL),((SELECT COUNT(*) FROM plan_change_proposals WHERE team_id=UNHEX(?) AND status='pending')+(SELECT COUNT(*) FROM project_change_proposals WHERE team_id=UNHEX(?) AND status='pending')+(SELECT COUNT(*) FROM member_change_proposals WHERE team_id=UNHEX(?) AND status='pending')),(SELECT COUNT(*) FROM team_memberships WHERE team_id=UNHEX(?) AND status='active'),(SELECT COUNT(*) FROM departments WHERE team_id=UNHEX(?) AND deleted_at IS NULL)`, tid, tid, tid, tid, tid, tid, tid, tid).Scan(&projects, &tasks, &blocked, &pending, &members, &departments)
	if e != nil {
		return e
	}
	write(w, 200, map[string]any{"projects": projects, "tasks": tasks, "blocked": blocked, "pending": pending, "members": members, "departments": departments, "role": role})
	return nil
}
func page(r *http.Request) (int, int) {
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if n < 1 {
		n = 1
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return n, size
}

func (s *Server) convertTeamKind(w http.ResponseWriter, r *http.Request, u User, tid, role string) error {
	if e := method(r, http.MethodPatch); e != nil {
		return e
	}
	if e := s.requireTeamAdmin(r, u, tid); e != nil {
		return e
	}
	var input struct {
		Kind string `json:"kind"`
	}
	if e := body(w, r, &input); e != nil {
		return e
	}
	if input.Kind != "team" && input.Kind != "company" {
		return bad(400, "VALIDATION_ERROR", "组织类型必须为团队或公司")
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = requireTeamAdminTx(r.Context(), tx, tid, u); e != nil {
		return e
	}
	var current string
	if e = tx.QueryRowContext(r.Context(), `SELECT kind FROM teams WHERE id=UNHEX(?) AND deleted_at IS NULL FOR UPDATE`, tid).Scan(&current); e != nil {
		return e
	}
	if current == input.Kind {
		write(w, 200, map[string]string{"kind": current})
		return nil
	}
	if input.Kind == "company" {
		var departmentID string
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM departments WHERE team_id=UNHEX(?) AND deleted_at IS NULL ORDER BY created_at LIMIT 1`, tid).Scan(&departmentID)
		if e == sql.ErrNoRows {
			departmentID = id()
			if _, e = tx.ExecContext(r.Context(), `INSERT INTO departments(id,team_id,name) VALUES(UNHEX(?),UNHEX(?),'默认部门')`, departmentID, tid); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE team_memberships SET department_id=UNHEX(?) WHERE team_id=UNHEX(?) AND department_id IS NULL`, departmentID, tid); e != nil {
			return e
		}
	} else {
		if _, e = tx.ExecContext(r.Context(), `UPDATE team_memberships SET department_id=NULL WHERE team_id=UNHEX(?)`, tid); e != nil {
			return e
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE projects SET department_id=NULL WHERE team_id=UNHEX(?)`, tid); e != nil {
			return e
		}
		if _, e = tx.ExecContext(r.Context(), `UPDATE departments SET deleted_at=UTC_TIMESTAMP(3) WHERE team_id=UNHEX(?) AND deleted_at IS NULL`, tid); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(r.Context(), `UPDATE teams SET kind=? WHERE id=UNHEX(?)`, input.Kind, tid); e != nil {
		return e
	}
	if e = addAudit(tx, u.ID, tid, "team.kind.change", "team", tid); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	write(w, 200, map[string]string{"kind": input.Kind})
	return nil
}
