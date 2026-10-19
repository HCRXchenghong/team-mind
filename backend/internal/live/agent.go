package live

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

// agentTools handles the explicit, user-invoked read-only task query endpoint.
// Model-selected proposal and sandbox tools are handled by agent_executor.go;
// shell and filesystem access stay confined to the separately authorized,
// offline sandbox and encrypted per-user workspace.
func (s *Server) agentTools(w http.ResponseWriter, r *http.Request, u User, path string) error {
	if path != "me/agent/tools/execute" {
		return bad(404, "NOT_FOUND", "资源不存在")
	}
	if e := method(r, "POST"); e != nil {
		return e
	}
	var x struct {
		Tool        string `json:"tool"`
		TeamID      string `json:"teamId"`
		ConfirmTeam bool   `json:"confirmTeam"`
		Limit       int    `json:"limit"`
	}
	if e := body(w, r, &x); e != nil {
		return e
	}
	limitInvalid := x.Limit < 0 || x.Limit > 20
	if x.Limit == 0 {
		x.Limit = 20
	}
	toolName := x.Tool
	if len(toolName) == 0 || len(toolName) > 100 {
		toolName = "invalid"
	}
	allowed := x.Tool == "list_team_tasks"

	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()

	var authorizedTeamID string
	var output []map[string]any
	var operationErr error
	callStatus := "succeeded"
	if toolName == "invalid" {
		operationErr = bad(400, "VALIDATION_ERROR", "工具名称无效")
		callStatus = "failed"
	} else if limitInvalid || x.Limit < 1 || x.Limit > 20 {
		operationErr = bad(400, "VALIDATION_ERROR", "查询数量必须在 1 到 20 之间")
		callStatus = "failed"
	} else if !x.ConfirmTeam || !validID(x.TeamID) {
		operationErr = bad(400, "TEAM_CONFIRMATION_REQUIRED", "请确认一个有效的当前团队上下文")
		callStatus = "denied"
	} else {
		// Lock the active membership while reading. A concurrent suspension or
		// removal must finish before the tool can return any team data.
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(m.team_id) FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=UNHEX(?) AND m.user_id=UNHEX(?) AND m.status='active' AND t.status='active' FOR UPDATE`, x.TeamID, u.ID).Scan(&authorizedTeamID)
		if e == sql.ErrNoRows {
			operationErr = bad(403, "TEAM_ACCESS_DENIED", "无权访问该团队")
			callStatus = "denied"
		} else if e != nil {
			return e
		} else if !allowed {
			// Authenticated members' denied tool attempts are linked to their
			// team audit log. A nonmember attempt remains unscoped.
			operationErr = bad(403, "AGENT_TOOL_DENIED", "该工具不在服务端允许列表中")
			callStatus = "denied"
		} else {
			rows, queryErr := tx.QueryContext(r.Context(), `SELECT HEX(id),title,status,priority,progress,due_date FROM tasks WHERE team_id=UNHEX(?) AND deleted_at IS NULL ORDER BY due_date IS NULL,due_date,updated_at DESC LIMIT ?`, authorizedTeamID, x.Limit)
			if queryErr != nil {
				return queryErr
			}
			output = make([]map[string]any, 0)
			for rows.Next() {
				var taskID, title, status, priority string
				var progress int
				var due sql.NullTime
				if e = rows.Scan(&taskID, &title, &status, &priority, &progress, &due); e != nil {
					rows.Close()
					return e
				}
				var dueDate any
				if due.Valid {
					dueDate = due.Time.Format("2006-01-02")
				}
				output = append(output, map[string]any{"id": taskID, "title": title, "status": status, "priority": priority, "progress": progress, "dueDate": dueDate})
			}
			if e = rows.Err(); e != nil {
				rows.Close()
				return e
			}
			if e = rows.Close(); e != nil {
				return e
			}
		}
	}

	callID := id()
	runID := id()
	args := map[string]any{"tool": toolName, "limit": x.Limit}
	if authorizedTeamID != "" {
		args["teamId"] = authorizedTeamID
	} else if validID(x.TeamID) {
		// A denied cross-team attempt should be auditable without retaining the
		// other team's raw identifier in the caller's run record.
		args["teamReference"] = hash(strings.ToLower(x.TeamID))
	}
	argsJSON, e := json.Marshal(args)
	if e != nil {
		return e
	}
	resultSummary := map[string]any{"count": len(output)}
	if operationErr != nil {
		resultSummary = map[string]any{"code": "DENIED_OR_INVALID"}
	}
	resultJSON, e := json.Marshal(resultSummary)
	if e != nil {
		return e
	}
	callTeam := any(nil)
	if authorizedTeamID != "" {
		callTeam = authorizedTeamID
	}
	status := "succeeded"
	if operationErr != nil {
		status = "failed"
	}
	if _, e = tx.ExecContext(r.Context(), `INSERT INTO agent_runs(id,user_id,team_id,provider,status,context_summary,started_at,finished_at) VALUES(UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),'internal-tool',?, ?,UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`, runID, u.ID, callTeam, callTeam, status, json.RawMessage(argsJSON)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(r.Context(), `INSERT INTO agent_tool_calls(id,run_id,user_id,team_id,tool_name,argument_summary,result_summary,approval_status,status) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),?,?,?,'not_required',?)`, callID, runID, u.ID, callTeam, callTeam, toolName, json.RawMessage(argsJSON), json.RawMessage(resultJSON), callStatus); e != nil {
		return e
	}
	action := "agent.tool.execute"
	if operationErr != nil {
		action = "agent.tool.denied"
	}
	if e = addAudit(tx, u.ID, authorizedTeamID, action, "agent_tool_call", callID); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if operationErr != nil {
		return operationErr
	}
	write(w, 200, map[string]any{"tool": toolName, "mode": "read_only", "count": len(output), "items": output})
	return nil
}
