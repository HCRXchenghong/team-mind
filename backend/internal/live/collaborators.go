package live

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
)

func activeTeamRoleTx(ctx context.Context, tx *sql.Tx, tid string, u User) (string, error) {
	var role, status string
	err := tx.QueryRowContext(ctx, `SELECT m.role,m.status FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=UNHEX(?) AND m.user_id=UNHEX(?) AND t.status='active' FOR UPDATE`, tid, u.ID).Scan(&role, &status)
	if err == sql.ErrNoRows || err == nil && status != "active" {
		return "", bad(403, "TEAM_ACCESS_DENIED", "团队成员关系已失效")
	}
	return role, err
}

func requireTeamAdminTx(ctx context.Context, tx *sql.Tx, tid string, u User) error {
	role, err := activeTeamRoleTx(ctx, tx, tid, u)
	if err != nil {
		return err
	}
	return admin(role)
}

func canContributeTaskTx(ctx context.Context, tx *sql.Tx, tid string, u User, t task) (bool, error) {
	role, err := activeTeamRoleTx(ctx, tx, tid, u)
	if err != nil {
		return false, err
	}
	if role == "team_admin" || role == "team_owner" || t.CreatedBy == u.ID || t.AssigneeID != nil && *t.AssigneeID == u.ID {
		return true, nil
	}
	var collaborator string
	err = tx.QueryRowContext(ctx, `SELECT HEX(user_id) FROM task_collaborators WHERE team_id=UNHEX(?) AND task_id=UNHEX(?) AND user_id=UNHEX(?) FOR UPDATE`, tid, t.ID, u.ID).Scan(&collaborator)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func canManageTaskTx(ctx context.Context, tx *sql.Tx, tid string, u User, t task) (bool, error) {
	role, err := activeTeamRoleTx(ctx, tx, tid, u)
	if err != nil {
		return false, err
	}
	return role == "team_admin" || role == "team_owner" || t.CreatedBy == u.ID || t.AssigneeID != nil && *t.AssigneeID == u.ID, nil
}

func (s *Server) collaborators(w http.ResponseWriter, r *http.Request, u User, tid, taskID, targetID string) error {
	_, err := s.taskByID(r, tid, taskID)
	if err != nil {
		return err
	}
	if r.Method == "GET" && targetID == "" {
		rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(c.user_id),u.display_name,m.status,HEX(c.added_by),c.created_at FROM task_collaborators c JOIN users u ON u.id=c.user_id JOIN team_memberships m ON m.team_id=c.team_id AND m.user_id=c.user_id WHERE c.team_id=UNHEX(?) AND c.task_id=UNHEX(?) ORDER BY u.display_name,u.id`, tid, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var memberID, name, status, addedBy string
			var createdAt sql.NullTime
			if err = rows.Scan(&memberID, &name, &status, &addedBy, &createdAt); err != nil {
				return err
			}
			out = append(out, map[string]any{"id": memberID, "name": name, "status": status, "addedBy": addedBy, "createdAt": createdAt.Time})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if r.Method == "POST" && targetID == "" {
		var input struct {
			UserID string `json:"userId"`
		}
		if err := body(w, r, &input); err != nil {
			return err
		}
		input.UserID = strings.ToUpper(strings.TrimSpace(input.UserID))
		if !validID(input.UserID) {
			return bad(400, "INVALID_ID", "协作者 ID 无效")
		}
		tx, err := s.DB.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		current, err := taskForUpdate(r.Context(), tx, tid, taskID)
		if err == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "任务不存在")
		}
		if err != nil {
			return err
		}
		allowed, err := canManageTaskTx(r.Context(), tx, tid, u, current)
		if err != nil {
			return err
		}
		if !allowed {
			return bad(403, "TASK_ACCESS_DENIED", "只有任务创建者、负责人或团队管理员可以管理协作者")
		}
		var status string
		err = tx.QueryRowContext(r.Context(), `SELECT status FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) FOR UPDATE`, tid, input.UserID).Scan(&status)
		if err == sql.ErrNoRows || err == nil && status != "active" {
			return bad(400, "COLLABORATOR_NOT_ACTIVE", "协作者必须是当前团队的有效成员")
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO task_collaborators(team_id,task_id,user_id,added_by) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?))`, tid, taskID, input.UserID, u.ID)
		if err != nil {
			return bad(409, "COLLABORATOR_EXISTS", "该协作者已在任务中")
		}
		if err = addAudit(tx, u.ID, tid, "task.collaborator.add", "task", taskID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		write(w, 201, map[string]bool{"added": true})
		return nil
	}
	if r.Method == "DELETE" && targetID != "" {
		if !validID(targetID) {
			return bad(404, "NOT_FOUND", "协作者不存在")
		}
		tx, err := s.DB.BeginTx(r.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		current, err := taskForUpdate(r.Context(), tx, tid, taskID)
		if err == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "任务不存在")
		}
		if err != nil {
			return err
		}
		allowed, err := canManageTaskTx(r.Context(), tx, tid, u, current)
		if err != nil {
			return err
		}
		if !allowed {
			return bad(403, "TASK_ACCESS_DENIED", "只有任务创建者、负责人或团队管理员可以管理协作者")
		}
		res, err := tx.ExecContext(r.Context(), `DELETE FROM task_collaborators WHERE team_id=UNHEX(?) AND task_id=UNHEX(?) AND user_id=UNHEX(?)`, tid, taskID, targetID)
		if err != nil {
			return err
		}
		count, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return bad(404, "NOT_FOUND", "协作者不存在")
		}
		if err = addAudit(tx, u.ID, tid, "task.collaborator.remove", "task", taskID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		write(w, 200, map[string]bool{"removed": true})
		return nil
	}
	return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}
