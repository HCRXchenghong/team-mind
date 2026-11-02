package live

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

// departments are internal units of one team/company. A member belongs to one
// primary department via team_memberships.department_id; projects may point at
// a department or stay cross-department (department_id NULL).
func (s *Server) departments(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {

	if len(p) == 3 && r.Method == http.MethodGet {
		rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(d.id),d.name,d.description,COALESCE(HEX(d.lead_user_id),''),COALESCE(l.display_name,''),
			(SELECT COUNT(*) FROM team_memberships m WHERE m.department_id=d.id AND m.status='active'),
			(SELECT COUNT(*) FROM projects p WHERE p.department_id=d.id AND p.deleted_at IS NULL)
			FROM departments d LEFT JOIN users l ON l.id=d.lead_user_id
			WHERE d.team_id=UNHEX(?) AND d.deleted_at IS NULL ORDER BY d.name`, tid)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []any{}
		for rows.Next() {
			var id, name, description, leadID, leadName string
			var members, projects int
			if err = rows.Scan(&id, &name, &description, &leadID, &leadName, &members, &projects); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "name": name, "description": description, "leadUserId": leadID, "leadName": leadName, "memberCount": members, "projectCount": projects})
		}
		write(w, 200, map[string]any{"items": items})
		return rows.Err()
	}
	if len(p) == 3 && r.Method == http.MethodPost {
		if err := s.requireTeamAdmin(r, u, tid); err != nil {
			return err
		}
		return s.createDepartment(w, r, u, tid)
	}
	if len(p) == 6 && p[4] == "members" && r.Method == http.MethodPatch {
		if err := s.requireDepartmentManager(r, u, tid, p[3]); err != nil {
			return err
		}
		return s.updateDepartmentMember(w, r, u, tid, p[3], p[5])
	}
	if len(p) >= 4 {
		departmentID := p[3]
		if !validID(departmentID) {
			return bad(404, "NOT_FOUND", "部门不存在")
		}
		if len(p) == 4 && r.Method == http.MethodPatch {
			if err := s.requireDepartmentManager(r, u, tid, departmentID); err != nil {
				return err
			}
			return s.updateDepartment(w, r, u, tid, departmentID)
		}
		if len(p) == 4 && r.Method == http.MethodDelete {
			if err := s.requireTeamAdmin(r, u, tid); err != nil {
				return err
			}
			return s.deleteDepartment(w, r, u, tid, departmentID)
		}
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}

func (s *Server) requireDepartmentManager(r *http.Request, u User, teamID, departmentID string) error {
	allowed, _, err := s.authorize(r.Context(), u.ID, authzManage, "team", teamID)
	if err != nil {
		return err
	}
	if allowed {
		return nil
	}
	var lead string
	err = s.DB.QueryRowContext(r.Context(), `SELECT COALESCE(HEX(lead_user_id),'') FROM departments WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, departmentID, teamID).Scan(&lead)
	if err == sql.ErrNoRows {
		return bad(404, "NOT_FOUND", "部门不存在")
	}
	if err != nil {
		return err
	}
	if lead == u.ID {
		return nil
	}
	return bad(403, "DEPARTMENT_ADMIN_REQUIRED", "需要团队管理员或部门负责人权限")
}

func (s *Server) createDepartment(w http.ResponseWriter, r *http.Request, u User, teamID string) error {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		LeadUserID  string `json:"leadUserId"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || len([]rune(input.Name)) > 120 || len([]rune(input.Description)) > 1000 {
		return bad(400, "VALIDATION_ERROR", "部门名称或描述无效")
	}
	if input.LeadUserID != "" {
		if err := s.validateDepartmentLead(r, teamID, input.LeadUserID); err != nil {
			return err
		}
	}
	exists, err := s.departmentNameTaken(r, teamID, input.Name, "")
	if err != nil {
		return err
	}
	if exists {
		return bad(409, "DEPARTMENT_NAME_EXISTS", "部门名称已存在")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	departmentID := id()
	var leadArg any
	if input.LeadUserID != "" {
		leadArg = input.LeadUserID
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO departments(id,team_id,name,description,lead_user_id) VALUES(UNHEX(?),UNHEX(?),?,?,IF(? IS NULL,NULL,UNHEX(?)))`, departmentID, teamID, input.Name, input.Description, leadArg, leadArg); err != nil {
		return err
	}
	if err = addAudit(tx, u.ID, teamID, "department.create", "department", departmentID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, 201, map[string]string{"id": departmentID})
	return nil
}

func (s *Server) updateDepartment(w http.ResponseWriter, r *http.Request, u User, teamID, departmentID string) error {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		LeadUserID  string `json:"leadUserId"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if input.Name == "" || len([]rune(input.Name)) > 120 || len([]rune(input.Description)) > 1000 {
		return bad(400, "VALIDATION_ERROR", "部门名称或描述无效")
	}
	if input.LeadUserID != "" {
		if err := s.validateDepartmentLead(r, teamID, input.LeadUserID); err != nil {
			return err
		}
	}
	exists, existsErr := s.departmentNameTaken(r, teamID, input.Name, departmentID)
	if existsErr != nil {
		return existsErr
	}
	if exists {
		return bad(409, "DEPARTMENT_NAME_EXISTS", "部门名称已存在")
	}
	var leadArg any
	if input.LeadUserID != "" {
		leadArg = input.LeadUserID
	}
	res, err := s.DB.ExecContext(r.Context(), `UPDATE departments SET name=?,description=?,lead_user_id=IF(? IS NULL,NULL,UNHEX(?)) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, input.Name, input.Description, leadArg, leadArg, departmentID, teamID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return bad(404, "NOT_FOUND", "部门不存在")
	}
	write(w, 200, map[string]bool{"saved": true})
	return nil
}

func (s *Server) deleteDepartment(w http.ResponseWriter, r *http.Request, u User, teamID, departmentID string) error {
	var members, projects int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT (SELECT COUNT(*) FROM team_memberships WHERE department_id=UNHEX(?) AND status='active'),(SELECT COUNT(*) FROM projects WHERE department_id=UNHEX(?) AND deleted_at IS NULL)`, departmentID, departmentID).Scan(&members, &projects); err != nil {
		return err
	}
	if members > 0 || projects > 0 {
		return bad(409, "DEPARTMENT_NOT_EMPTY", "请先转移该部门的成员和项目")
	}
	res, err := s.DB.ExecContext(r.Context(), `UPDATE departments SET deleted_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, departmentID, teamID)
	if err != nil {
		return err
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return bad(404, "NOT_FOUND", "部门不存在")
	}
	return nil
}

func (s *Server) validateDepartmentLead(r *http.Request, teamID, userID string) error {
	var role string
	err := s.DB.QueryRowContext(r.Context(), `SELECT role FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, teamID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return bad(400, "VALIDATION_ERROR", "部门负责人必须是有效公司成员")
	}
	return err
}

func (s *Server) departmentNameTaken(r *http.Request, teamID, name, excludeID string) (bool, error) {
	var count int
	var exclude any
	if excludeID != "" {
		exclude = excludeID
	}
	err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM departments WHERE team_id=UNHEX(?) AND deleted_at IS NULL
		AND LOWER(TRIM(name))=LOWER(TRIM(?)) AND (? IS NULL OR id<>UNHEX(?))`, teamID, name, exclude, exclude).Scan(&count)
	return count > 0, err
}

func (s *Server) updateDepartmentMember(w http.ResponseWriter, r *http.Request, u User, teamID, departmentID, targetUserID string) error {
	if !validID(targetUserID) {
		return bad(400, "INVALID_ID", "成员 ID 无效")
	}
	var input struct {
		DepartmentID string `json:"departmentId"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.DepartmentID = strings.ToUpper(strings.TrimSpace(input.DepartmentID))
	allowed, _, err := s.authorize(r.Context(), u.ID, authzManage, "team", teamID)
	if err != nil {
		return err
	}
	isAdmin := allowed
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentDepartment, targetRole string
	err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(HEX(department_id),''),role FROM team_memberships
		WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND status IN ('active','suspended') FOR UPDATE`, teamID, targetUserID).Scan(&currentDepartment, &targetRole)
	if err == sql.ErrNoRows {
		return bad(404, "NOT_FOUND", "成员不存在")
	}
	if err != nil {
		return err
	}
	if !isAdmin {
		if targetRole == "team_owner" {
			return bad(403, "TEAM_OWNER_REQUIRED", "不能调整公司所有者的部门")
		}
		if input.DepartmentID != "" && input.DepartmentID != strings.ToUpper(departmentID) {
			return bad(403, "DEPARTMENT_ADMIN_REQUIRED", "部门负责人只能移出成员或接纳未分配成员")
		}
		if currentDepartment != "" && currentDepartment != strings.ToUpper(departmentID) {
			return bad(403, "DEPARTMENT_ADMIN_REQUIRED", "该成员不属于你负责的部门")
		}
	} else if input.DepartmentID != "" {
		var exists int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM departments WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, input.DepartmentID, teamID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return bad(400, "VALIDATION_ERROR", "目标部门不存在")
		}
	}
	var departmentArg any
	if input.DepartmentID != "" {
		departmentArg = input.DepartmentID
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE team_memberships SET department_id=IF(? IS NULL,NULL,UNHEX(?)),version=version+1 WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, departmentArg, departmentArg, teamID, targetUserID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE departments SET lead_user_id=NULL WHERE team_id=UNHEX(?) AND lead_user_id=UNHEX(?) AND (id<>COALESCE(UNHEX(?),'') OR ? IS NULL)`, teamID, targetUserID, departmentArg, departmentArg); err != nil {
		return err
	}
	if err = addAudit(tx, u.ID, teamID, "member.department_update", "user", targetUserID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, 200, map[string]bool{"saved": true})
	return nil
}

func (s *Server) teamKind(ctx context.Context, teamID string) (string, error) {
	var kind string
	err := s.DB.QueryRowContext(ctx, `SELECT kind FROM teams WHERE id=UNHEX(?) AND deleted_at IS NULL`, teamID).Scan(&kind)
	return kind, err
}
