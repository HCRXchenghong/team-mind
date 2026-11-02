package live

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// agentRuns exposes a narrow control surface over the caller's own runs:
// list, inspect and cancel. It never returns instructions or tool arguments.
func (s *Server) agentRuns(w http.ResponseWriter, r *http.Request, u User, path string) error {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 4 && parts[0] == "me" && parts[1] == "agent-runs" && parts[3] == "trace" {
		if e := method(r, "GET"); e != nil {
			return e
		}
		if !validID(parts[2]) {
			return bad(400, "INVALID_ID", "运行 ID 无效")
		}
		return s.agentRunTrace(w, r, u, parts[2])
	}
	if len(parts) == 4 && parts[0] == "me" && parts[1] == "agent-runs" && parts[3] == "resume" {
		if e := method(r, "POST"); e != nil {
			return e
		}
		if !validID(parts[2]) {
			return bad(400, "INVALID_ID", "运行 ID 无效")
		}
		return s.resumeAgentRun(w, r, u, parts[2])
	}
	if len(parts) == 4 && parts[0] == "me" && parts[1] == "agent-runs" && parts[3] == "cancel" {
		if e := method(r, "POST"); e != nil {
			return e
		}
		if !validID(parts[2]) {
			return bad(400, "INVALID_ID", "运行 ID 无效")
		}
		if e := s.cancelAgentRun(r.Context(), u.ID, parts[2]); e != nil {
			return e
		}
		write(w, 200, map[string]any{"id": parts[2], "status": "cancelled"})
		return nil
	}
	if len(parts) == 2 && parts[0] == "me" && parts[1] == "agent-runs" {
		if e := method(r, "GET"); e != nil {
			return e
		}
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(id),provider,COALESCE(model,''),status,context_summary,
			started_at,finished_at,COALESCE(TIMESTAMPDIFF(MICROSECOND,started_at,finished_at)/1000,0)
			FROM agent_runs WHERE user_id=UNHEX(?) ORDER BY created_at DESC LIMIT 20`, u.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		items := make([]map[string]any, 0)
		for rows.Next() {
			item, e := scanAgentRunView(rows)
			if e != nil {
				return e
			}
			items = append(items, item)
		}
		write(w, 200, items)
		return rows.Err()
	}
	if len(parts) == 3 && parts[0] == "me" && parts[1] == "agent-runs" {
		if e := method(r, "GET"); e != nil {
			return e
		}
		runID := parts[2]
		if !validID(runID) {
			return bad(400, "INVALID_ID", "运行 ID 无效")
		}
		row := s.DB.QueryRowContext(r.Context(), `SELECT HEX(id),provider,COALESCE(model,''),status,context_summary,
			started_at,finished_at,COALESCE(TIMESTAMPDIFF(MICROSECOND,started_at,finished_at)/1000,0)
			FROM agent_runs WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, runID, u.ID)
		item, e := scanAgentRunView(row)
		if e == sql.ErrNoRows {
			return bad(404, "RUN_NOT_FOUND", "运行记录不存在")
		}
		if e != nil {
			return e
		}
		write(w, 200, item)
		return nil
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}

type agentRunScanner interface {
	Scan(dest ...any) error
}

func scanAgentRunView(scanner agentRunScanner) (map[string]any, error) {
	var runID, provider, model, status string
	var summary []byte
	var startedAt, finishedAt sql.NullTime
	var durationMS float64
	if e := scanner.Scan(&runID, &provider, &model, &status, &summary, &startedAt, &finishedAt, &durationMS); e != nil {
		return nil, e
	}
	var start, finish any
	if startedAt.Valid {
		start = startedAt.Time
	}
	if finishedAt.Valid {
		finish = finishedAt.Time
	}
	view := agentRunView(runID, provider, model, status, summary, start, finish)
	view["durationMs"] = durationMS
	return view, nil
}

// resumeAgentRun only replays runs whose completed steps are read-only. Write
// tools (create_task, proposals, memory candidates, sandbox) are refused: a
// blind replay would duplicate side effects. This is intentionally a safe
// subset of durable execution, not a pretend full replay.
func (s *Server) resumeAgentRun(w http.ResponseWriter, r *http.Request, u User, runID string) error {
	var status, teamHex, conversationHex string
	var summary []byte
	err := s.DB.QueryRowContext(r.Context(), `SELECT status,COALESCE(HEX(team_id),''),COALESCE(HEX(conversation_id),''),context_summary
		FROM agent_runs WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, runID, u.ID).Scan(&status, &teamHex, &conversationHex, &summary)
	if err == sql.ErrNoRows {
		return bad(404, "RUN_NOT_FOUND", "运行记录不存在")
	}
	if err != nil {
		return err
	}
	if status == "running" {
		return bad(409, "RUN_STILL_RUNNING", "该运行仍在执行；请先取消")
	}
	if status == "succeeded" {
		return bad(409, "RUN_ALREADY_COMPLETED", "该运行已成功完成，无需恢复")
	}
	var parsed struct {
		Checkpoint struct {
			Phase       string `json:"phase"`
			Instruction string `json:"instruction"`
			Steps       []struct {
				Tool string `json:"tool"`
			} `json:"steps"`
			Messages []aiMessage `json:"messages"`
			Options  struct {
				ConfirmTeam    bool `json:"confirmTeam"`
				AllowWebAccess bool `json:"allowWebAccess"`
			} `json:"options"`
		} `json:"checkpoint"`
	}
	if len(summary) == 0 || json.Unmarshal(summary, &parsed) != nil {
		return bad(409, "RUN_NOT_RESUMABLE", "该运行没有可用的 checkpoint")
	}
	cp := parsed.Checkpoint
	if strings.TrimSpace(cp.Instruction) == "" {
		return bad(409, "RUN_NOT_RESUMABLE", "checkpoint 缺少原始指令")
	}
	if cp.Phase == "completed" {
		return bad(409, "RUN_ALREADY_COMPLETED", "该运行已完成，无需恢复")
	}
	hasWeb := false
	initialSteps := make([]agentToolStep, 0, len(cp.Steps))
	for _, step := range cp.Steps {
		initialSteps = append(initialSteps, agentToolStep{Tool: step.Tool})
		if step.Tool == "web_search" || step.Tool == "web_fetch" {
			hasWeb = true
		}
	}
	// Without a message checkpoint we can only safely replay read-only tools.
	// With messages, completed steps (including writes) are already reflected in
	// the conversation and are never executed again.
	if len(cp.Messages) == 0 {
		writeTools := map[string]bool{
			"create_task": true, "propose_task_change": true, "propose_project_change": true,
			"propose_member_change": true, "create_team_memory_candidate": true,
			"run_python": true, "run_shell": true,
		}
		for _, step := range cp.Steps {
			if writeTools[step.Tool] {
				return bad(409, "RUN_NOT_RESUMABLE", "该运行已执行写入类工具（"+step.Tool+"），为避免重复副作用不能自动恢复")
			}
		}
	}
	teamID := sql.NullString{}
	if teamHex != "" {
		teamID = sql.NullString{String: teamHex, Valid: true}
	}
	confirm := cp.Options.ConfirmTeam || teamID.Valid
	web := cp.Options.AllowWebAccess || hasWeb
	input := agentExecutionInput{
		Instruction:    cp.Instruction,
		ConfirmTeam:    confirm,
		AllowWebAccess: web,
	}
	var resp agentExecutionResponse
	if len(cp.Messages) > 0 {
		err = s.runAgentExecutionState(r.Context(), u, conversationHex, teamID, input, &resp, cp.Messages, initialSteps, true)
	} else {
		err = s.runAgentExecution(r.Context(), u, conversationHex, teamID, input, &resp)
	}
	if err != nil {
		return err
	}
	_, _ = s.DB.ExecContext(r.Context(), `UPDATE agent_runs SET context_summary=JSON_SET(context_summary,'$.resumedAt',UTC_TIMESTAMP(3),'$.resumedByRunId',?)
		WHERE id=UNHEX(?)`, resp.RunID, runID)
	write(w, 200, map[string]any{"resumedFrom": runID, "run": resp})
	return nil
}

func (s *Server) agentRunTrace(w http.ResponseWriter, r *http.Request, u User, runID string) error {
	var status, provider, model string
	var summary []byte
	var startedAt, finishedAt sql.NullTime
	var durationMS float64
	err := s.DB.QueryRowContext(r.Context(), `SELECT status,provider,COALESCE(model,''),context_summary,started_at,finished_at,
		COALESCE(TIMESTAMPDIFF(MICROSECOND,started_at,finished_at)/1000,0)
		FROM agent_runs WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, runID, u.ID).Scan(
		&status, &provider, &model, &summary, &startedAt, &finishedAt, &durationMS)
	if err == sql.ErrNoRows {
		return bad(404, "RUN_NOT_FOUND", "运行记录不存在")
	}
	if err != nil {
		return err
	}
	spans := []map[string]any{}
	spans = append(spans, map[string]any{
		"type": "run", "name": "agent.run", "status": status,
		"startedAt": startedAt, "finishedAt": finishedAt, "durationMs": durationMS,
	})
	rows, err := s.DB.QueryContext(r.Context(), `SELECT tool_name,status,approval_status,created_at
		FROM agent_tool_calls WHERE run_id=UNHEX(?) ORDER BY created_at,id`, runID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tool, toolStatus, approvalStatus string
		var at time.Time
		if err = rows.Scan(&tool, &toolStatus, &approvalStatus, &at); err != nil {
			rows.Close()
			return err
		}
		spans = append(spans, map[string]any{
			"type": "tool", "name": tool, "status": toolStatus,
			"approvalStatus": approvalStatus, "startedAt": at,
		})
	}
	rows.Close()
	var parsed struct {
		Approval struct {
			Type, ID, Status, Decision string
		} `json:"approval"`
	}
	if len(summary) > 0 {
		_ = json.Unmarshal(summary, &parsed)
	}
	if parsed.Approval.Type != "" {
		spans = append(spans, map[string]any{
			"type": "approval", "name": parsed.Approval.Type,
			"status": parsed.Approval.Status, "decision": parsed.Approval.Decision,
		})
	}
	rows, err = s.DB.QueryContext(r.Context(), `SELECT status,attempts,decision,created_at,updated_at,last_error
		FROM agent_followups WHERE run_id=UNHEX(?) ORDER BY created_at`, runID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var followStatus, decision, lastError string
		var attempts int
		var createdAt, updatedAt time.Time
		if err = rows.Scan(&followStatus, &attempts, &decision, &createdAt, &updatedAt, &lastError); err != nil {
			rows.Close()
			return err
		}
		spans = append(spans, map[string]any{
			"type": "followup", "name": "approval.followup", "status": followStatus,
			"attempts": attempts, "decision": decision, "lastError": truncateRunes(lastError, 200),
			"startedAt": createdAt, "finishedAt": updatedAt,
		})
	}
	rows.Close()
	write(w, 200, map[string]any{
		"traceId": runID, "provider": provider, "model": model,
		"spans": spans,
	})
	return nil
}
