package live

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

var tupleRelations = map[authzRelation][]string{
	authzView:       {"member", "admin", "owner", "lead", "creator", "assignee", "collaborator"},
	authzContribute: {"member", "admin", "owner", "lead", "creator", "assignee", "collaborator", "editor"},
	authzApprove:    {"admin", "owner", "lead", "approver"},
	authzManage:     {"admin", "owner", "lead", "manager"},
}

func teamRoleFromTuple(relation string) string {
	switch relation {
	case "owner":
		return "team_owner"
	case "admin":
		return "team_admin"
	case "member":
		return "member"
	default:
		return ""
	}
}

func tupleRelationForRole(role string) string {
	switch role {
	case "team_owner":
		return "owner"
	case "team_admin":
		return "admin"
	case "member":
		return "member"
	default:
		return ""
	}
}

func (s *Server) tupleTeamRole(ctx context.Context, userID, teamID string) (string, error) {
	var role string
	err := s.DB.QueryRowContext(ctx, `SELECT relation FROM relation_tuples
		WHERE resource_type='team' AND resource_id=UNHEX(?) AND subject_type='user' AND subject_id=UNHEX(?)
		AND relation IN ('owner','admin','member')
		ORDER BY FIELD(relation,'owner','admin','member') LIMIT 1`, teamID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return role, err
}

func (s *Server) tupleTeamID(ctx context.Context, objectType, objectID string) (string, error) {
	if objectType == "team" {
		return objectID, nil
	}
	var query string
	switch objectType {
	case "project":
		query = `SELECT HEX(team_id) FROM projects WHERE id=UNHEX(?) AND deleted_at IS NULL`
	case "task":
		query = `SELECT HEX(team_id) FROM tasks WHERE id=UNHEX(?) AND deleted_at IS NULL`
	case "department":
		query = `SELECT HEX(team_id) FROM departments WHERE id=UNHEX(?) AND deleted_at IS NULL`
	default:
		return "", sql.ErrNoRows
	}
	var teamID string
	err := s.DB.QueryRowContext(ctx, query, objectID).Scan(&teamID)
	return teamID, err
}

func (s *Server) tupleDirect(ctx context.Context, userID, objectType, objectID string, relations []string) (bool, error) {
	if len(relations) == 0 {
		return false, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(relations)), ",")
	args := []any{objectType, objectID, userID}
	for _, relation := range relations {
		args = append(args, relation)
	}
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM relation_tuples
		WHERE resource_type=? AND resource_id=UNHEX(?) AND subject_type='user' AND subject_id=UNHEX(?)
		AND relation IN (`+placeholders+`)`, args...).Scan(&count)
	return count > 0, err
}

func (s *Server) tupleProjectDepartmentLead(ctx context.Context, userID, projectID string) (bool, error) {
	var count int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM relation_tuples p
		JOIN relation_tuples d ON d.resource_type='department' AND d.resource_id=p.subject_id
		WHERE p.resource_type='project' AND p.resource_id=UNHEX(?) AND p.relation='department'
		AND d.relation='lead' AND d.subject_type='user' AND d.subject_id=UNHEX(?)`, projectID, userID).Scan(&count)
	return count > 0, err
}

// tupleCheck is the relationship Check: direct subject tuples, then the
// parent edges (project→team, task→project, department→team).
func (s *Server) tupleCheck(ctx context.Context, userID string, relation authzRelation, objectType, objectID string) (bool, error) {
	if objectID == "" || !validID(objectID) {
		return false, nil
	}
	teamID, err := s.tupleTeamID(ctx, objectType, objectID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var status string
	if err = s.DB.QueryRowContext(ctx, `SELECT status FROM teams WHERE id=UNHEX(?) AND deleted_at IS NULL`, teamID).Scan(&status); err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if status != "active" {
		return false, nil
	}
	role, err := s.tupleTeamRole(ctx, userID, teamID)
	if err != nil {
		return false, err
	}
	isAdmin := role == "owner" || role == "admin"
	isMember := role != ""
	if objectType == "team" {
		return roleAllows(teamRoleFromTuple(role), relation), nil
	}
	switch objectType {
	case "project":
		if relation == authzManage || relation == authzApprove {
			if isAdmin {
				return true, nil
			}
			return s.tupleProjectDepartmentLead(ctx, userID, objectID)
		}
		return isMember, nil
	case "task":
		if relation == authzManage || relation == authzApprove {
			return isAdmin, nil
		}
		if relation == authzView {
			return isMember, nil
		}
		if isAdmin {
			return true, nil
		}
		return s.tupleDirect(ctx, userID, "task", objectID, []string{"creator", "assignee", "collaborator"})
	case "department":
		if relation == authzManage || relation == authzApprove {
			if isAdmin {
				return true, nil
			}
			return s.tupleDirect(ctx, userID, "department", objectID, []string{"lead"})
		}
		return isMember, nil
	default:
		return false, nil
	}
}

func (s *Server) tupleListObjects(ctx context.Context, userID string, relation authzRelation, objectType, teamID string) ([]map[string]any, error) {
	items := []map[string]any{}
	type candidate struct{ id, name string }
	var candidates []candidate
	var query string
	switch objectType {
	case "team":
		rows, err := s.DB.QueryContext(ctx, `SELECT HEX(t.id),t.name FROM teams t WHERE t.deleted_at IS NULL AND t.status='active' ORDER BY t.name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name string
			if err = rows.Scan(&id, &name); err != nil {
				return nil, err
			}
			candidates = append(candidates, candidate{id, name})
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
	case "project":
		query = `SELECT HEX(id),name FROM projects WHERE team_id=UNHEX(?) AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 200`
	case "task":
		query = `SELECT HEX(id),title FROM tasks WHERE team_id=UNHEX(?) AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 200`
	case "department":
		query = `SELECT HEX(id),name FROM departments WHERE team_id=UNHEX(?) AND deleted_at IS NULL ORDER BY name LIMIT 200`
	default:
		return items, nil
	}
	if query != "" {
		rows, err := s.DB.QueryContext(ctx, query, teamID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name string
			if err = rows.Scan(&id, &name); err != nil {
				return nil, err
			}
			candidates = append(candidates, candidate{id, name})
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
	}
	for _, item := range candidates {
		allowed, err := s.tupleCheck(ctx, userID, relation, objectType, item.id)
		if err != nil {
			return nil, err
		}
		if allowed {
			items = append(items, map[string]any{"id": item.id, "name": item.name})
		}
	}
	return items, nil
}

func (s *Server) teamAuthz(w http.ResponseWriter, r *http.Request, u User, teamID string) error {
	if e := method(r, http.MethodGet); e != nil {
		return e
	}
	allowed, err := s.tupleCheck(r.Context(), u.ID, authzView, "team", teamID)
	if err != nil {
		return err
	}
	if !allowed {
		return bad(http.StatusForbidden, "TEAM_ACCESS_DENIED", "无权访问该公司")
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/teams/"+teamID+"/authz/")
	switch path {
	case "check":
		relation := authzRelation(r.URL.Query().Get("relation"))
		objectType := r.URL.Query().Get("objectType")
		objectID := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("objectId")))
		valid := false
		for _, candidate := range authzRelations {
			if candidate == relation {
				valid = true
				break
			}
		}
		if !valid || !in(objectType, "team", "project", "task", "department") || !validID(objectID) {
			return bad(400, "VALIDATION_ERROR", "权限检查参数无效")
		}
		result, err := s.tupleCheck(r.Context(), u.ID, relation, objectType, objectID)
		if err != nil {
			return err
		}
		write(w, 200, map[string]any{"allowed": result, "relation": relation, "objectType": objectType, "objectId": objectID})
		return nil
	case "objects":
		relation := authzRelation(r.URL.Query().Get("relation"))
		objectType := r.URL.Query().Get("type")
		valid := false
		for _, candidate := range authzRelations {
			if candidate == relation {
				valid = true
				break
			}
		}
		if !valid || !in(objectType, "team", "project", "task", "department") {
			return bad(400, "VALIDATION_ERROR", "对象查询参数无效")
		}
		items, err := s.tupleListObjects(r.Context(), u.ID, relation, objectType, teamID)
		if err != nil {
			return err
		}
		write(w, 200, map[string]any{"relation": relation, "type": objectType, "items": items})
		return nil
	default:
		return bad(404, "NOT_FOUND", "权限接口不存在")
	}
}
