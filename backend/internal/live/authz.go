package live

import (
	"context"
	"database/sql"
	"net/http"
)

// A deliberately small relationship model for the resources TeamMind actually
// has. It is the policy contract used by the AI tool boundary and exposed to
// clients; API handlers keep their existing inline checks as defense in depth.
type authzRelation string

const (
	authzView       authzRelation = "can_view"
	authzContribute authzRelation = "can_contribute"
	authzApprove    authzRelation = "can_approve"
	authzManage     authzRelation = "can_manage"
)

var authzRelations = []authzRelation{authzView, authzContribute, authzApprove, authzManage}

func roleAllows(role string, relation authzRelation) bool {
	switch role {
	case "team_owner", "team_admin":
		return relation == authzView || relation == authzContribute || relation == authzApprove || relation == authzManage
	case "member":
		return relation == authzView || relation == authzContribute
	default:
		return false
	}
}

func (s *Server) teamPolicy(ctx context.Context, userID, teamID string) (role, teamStatus, memberStatus string, err error) {
	err = s.DB.QueryRowContext(ctx, `SELECT t.status,COALESCE(m.role,''),COALESCE(m.status,'')
		FROM teams t LEFT JOIN team_memberships m ON m.team_id=t.id AND m.user_id=UNHEX(?)
		WHERE t.id=UNHEX(?) AND t.deleted_at IS NULL`, userID, teamID).Scan(&teamStatus, &role, &memberStatus)
	return role, teamStatus, memberStatus, err
}

func (s *Server) authorize(ctx context.Context, userID string, relation authzRelation, objectType, objectID string) (bool, string, error) {
	if objectID == "" || !validID(objectID) {
		return false, "", nil
	}
	allowed, err := s.tupleCheck(ctx, userID, relation, objectType, objectID)
	if err != nil {
		return false, "", err
	}
	return allowed, "", nil
}

func (s *Server) teamPermissions(w http.ResponseWriter, r *http.Request, userID, teamID string) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	role, teamStatus, memberStatus, err := s.teamPolicy(r.Context(), userID, teamID)
	if err == sql.ErrNoRows {
		return bad(404, "NOT_FOUND", "公司不存在")
	}
	if err != nil {
		return err
	}
	active := teamStatus == "active" && memberStatus == "active"
	permissions := map[string]bool{}
	relations := make([]string, 0, len(authzRelations))
	for _, relation := range authzRelations {
		allowed := false
		if active {
			allowed, err = s.tupleCheck(r.Context(), userID, relation, "team", teamID)
			if err != nil {
				return err
			}
		}
		permissions[string(relation)] = allowed
		if allowed {
			relations = append(relations, string(relation))
		}
	}
	write(w, 200, map[string]any{"teamId": teamID, "role": role, "permissions": permissions, "relations": relations})
	return nil
}

var agentWriteTools = map[string]bool{
	"create_task": true, "propose_task_change": true, "propose_project_change": true,
	"propose_member_change": true, "create_team_memory_candidate": true, "send_agent_message": true,
}

func (s *Server) requireTeamAdmin(r *http.Request, u User, teamID string) error {
	allowed, _, err := s.authorize(r.Context(), u.ID, authzManage, "team", teamID)
	if err != nil {
		return err
	}
	if !allowed {
		return bad(403, "TEAM_ADMIN_REQUIRED", "需要公司管理员权限")
	}
	return nil
}

// listAuthorizedTeams is the ListObjects counterpart: which teams does this
// user hold a given relation on? It is intentionally backed by the same
// roleAllows policy as Check.
func (s *Server) listAuthorizedTeams(w http.ResponseWriter, r *http.Request, u User) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	relation := authzRelation(r.URL.Query().Get("relation"))
	valid := false
	for _, candidate := range authzRelations {
		if candidate == relation {
			valid = true
			break
		}
	}
	if !valid {
		return bad(400, "VALIDATION_ERROR", "权限关系无效")
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(t.id),t.name,m.role FROM teams t
		JOIN team_memberships m ON m.team_id=t.id AND m.user_id=UNHEX(?)
		WHERE t.deleted_at IS NULL AND t.status='active' AND m.status='active'
		ORDER BY t.name`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, role string
		if err = rows.Scan(&id, &name, &role); err != nil {
			return err
		}
		if roleAllows(role, relation) {
			items = append(items, map[string]any{"id": id, "name": name, "role": role})
		}
	}
	write(w, 200, map[string]any{"relation": string(relation), "items": items})
	return rows.Err()
}
