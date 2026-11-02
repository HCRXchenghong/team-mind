package live

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

type teamAccessPolicy struct {
	AIAccess      string `json:"aiAccess"`
	WechatAccess  string `json:"wechatAccess"`
	DataIsolation string `json:"dataIsolation"`
}

func defaultTeamAccessPolicy() teamAccessPolicy {
	return teamAccessPolicy{AIAccess: "all", WechatAccess: "all", DataIsolation: "shared"}
}

func (s *Server) loadTeamAccessPolicy(ctx context.Context, teamID string) (teamAccessPolicy, error) {
	policy := defaultTeamAccessPolicy()
	err := s.DB.QueryRowContext(ctx, `SELECT ai_access,wechat_access,data_isolation FROM team_access_policies WHERE team_id=UNHEX(?)`, teamID).Scan(&policy.AIAccess, &policy.WechatAccess, &policy.DataIsolation)
	if err == sql.ErrNoRows {
		return policy, nil
	}
	return policy, err
}

func (s *Server) teamMemberRole(ctx context.Context, userID, teamID string) (string, error) {
	var role string
	err := s.DB.QueryRowContext(ctx, `SELECT m.role FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=UNHEX(?) AND m.user_id=UNHEX(?) AND m.status='active' AND t.status='active' AND t.deleted_at IS NULL`, teamID, userID).Scan(&role)
	return role, err
}

func (s *Server) isTeamUnitLead(ctx context.Context, userID, teamID string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM departments WHERE team_id=UNHEX(?) AND lead_user_id=UNHEX(?) AND deleted_at IS NULL`, teamID, userID).Scan(&count)
	return count > 0, err
}

// accessAllowed implements the configurable all/leads/admins policy.
func (s *Server) accessAllowed(ctx context.Context, userID, teamID, access string) (bool, error) {
	if access == "" || access == "all" {
		return true, nil
	}
	role, err := s.teamMemberRole(ctx, userID, teamID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if role == "team_owner" || role == "team_admin" {
		return true, nil
	}
	if access == "admins" {
		return false, nil
	}
	lead, err := s.isTeamUnitLead(ctx, userID, teamID)
	if err != nil {
		return false, err
	}
	return lead, nil
}

func (s *Server) aiAccessAllowed(ctx context.Context, userID, teamID string) (bool, error) {
	policy, err := s.loadTeamAccessPolicy(ctx, teamID)
	if err != nil {
		return false, err
	}
	return s.accessAllowed(ctx, userID, teamID, policy.AIAccess)
}

func (s *Server) wechatAccessAllowed(ctx context.Context, userID, teamID string) (bool, error) {
	policy, err := s.loadTeamAccessPolicy(ctx, teamID)
	if err != nil {
		return false, err
	}
	return s.accessAllowed(ctx, userID, teamID, policy.WechatAccess)
}

// dataUnitForUser returns the department/group that should be forced for a
// non-admin member when data isolation is enabled. Empty means no restriction.
func (s *Server) dataUnitForUser(ctx context.Context, userID, teamID string) (string, error) {
	policy, err := s.loadTeamAccessPolicy(ctx, teamID)
	if err != nil {
		return "", err
	}
	if policy.DataIsolation != "units" {
		return "", nil
	}
	role, err := s.teamMemberRole(ctx, userID, teamID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if role == "team_owner" || role == "team_admin" {
		return "", nil
	}
	var unit string
	err = s.DB.QueryRowContext(ctx, `SELECT COALESCE(HEX(department_id),'') FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, teamID, userID).Scan(&unit)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if unit == "" {
		// Isolated members without a unit must not fall back to team-wide data.
		return "00000000000000000000000000000000", nil
	}
	return unit, nil
}

func (s *Server) teamAccessPolicyRoute(w http.ResponseWriter, r *http.Request, u User, teamID, role string) error {
	if admin(role) != nil {
		return bad(http.StatusForbidden, "TEAM_ADMIN_REQUIRED", "需要公司/团队管理员权限")
	}
	if r.Method == http.MethodGet {
		policy, err := s.loadTeamAccessPolicy(r.Context(), teamID)
		if err != nil {
			return err
		}
		write(w, http.StatusOK, policy)
		return nil
	}
	if r.Method != http.MethodPatch {
		return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
	}
	var input struct {
		AIAccess      string `json:"aiAccess"`
		WechatAccess  string `json:"wechatAccess"`
		DataIsolation string `json:"dataIsolation"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	valid := func(v string) bool { return v == "all" || v == "leads" || v == "admins" }
	if !valid(input.AIAccess) || !valid(input.WechatAccess) || !in(input.DataIsolation, "shared", "units") {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "权限值无效")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO team_access_policies(team_id,ai_access,wechat_access,data_isolation,updated_by) VALUES(UNHEX(?),?,?,?,UNHEX(?)) ON DUPLICATE KEY UPDATE ai_access=VALUES(ai_access),wechat_access=VALUES(wechat_access),data_isolation=VALUES(data_isolation),updated_by=VALUES(updated_by)`, teamID, strings.ToLower(input.AIAccess), strings.ToLower(input.WechatAccess), strings.ToLower(input.DataIsolation), u.ID)
	if err != nil {
		return err
	}
	if err = addAudit(tx, u.ID, teamID, "team.access_policy_updated", "team", teamID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	policy, err := s.loadTeamAccessPolicy(r.Context(), teamID)
	if err != nil {
		return err
	}
	write(w, http.StatusOK, policy)
	return nil
}

func (s *Server) unitAccessToProject(ctx context.Context, userID, teamID, projectID string) (bool, error) {
	unit, err := s.dataUnitForUser(ctx, userID, teamID)
	if err != nil || unit == "" {
		return err == nil, err
	}
	var department string
	err = s.DB.QueryRowContext(ctx, `SELECT COALESCE(HEX(department_id),'') FROM projects WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, projectID, teamID).Scan(&department)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil && department != "" && department == unit, err
}

func (s *Server) unitAccessToTask(ctx context.Context, userID, teamID, taskID string) (bool, error) {
	unit, err := s.dataUnitForUser(ctx, userID, teamID)
	if err != nil || unit == "" {
		return err == nil, err
	}
	var department string
	err = s.DB.QueryRowContext(ctx, `SELECT COALESCE(HEX(p.department_id),'') FROM tasks t LEFT JOIN projects p ON p.id=t.project_id WHERE t.id=UNHEX(?) AND t.team_id=UNHEX(?) AND t.deleted_at IS NULL`, taskID, teamID).Scan(&department)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil && department != "" && department == unit, err
}
