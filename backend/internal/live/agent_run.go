package live

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"
)

// Run lifecycle records answer "what ran, with which model, how long, and did
// it finish" without storing prompts or tool payloads. A small checkpoint is
// attached after each step so an interrupted run can be diagnosed (and, in a
// later iteration, resumed) without replaying side effects blindly.
func (s *Server) startAgentRun(ctx context.Context, userID, teamID, conversationID, provider, purpose string) (string, context.Context, context.CancelFunc, error) {
	runID := id()
	runCtx, cancel := context.WithCancel(ctx)
	var teamArg, conversationArg any
	if teamID != "" {
		teamArg = teamID
	}
	if conversationID != "" {
		conversationArg = conversationID
	}
	summary, err := json.Marshal(map[string]any{"purpose": purpose, "startedAt": time.Now().UTC()})
	if err != nil {
		cancel()
		return "", nil, nil, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO agent_runs(id,user_id,team_id,conversation_id,provider,status,context_summary,started_at)
		VALUES(UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),IF(? IS NULL,NULL,UNHEX(?)),?,'running',?,UTC_TIMESTAMP(3))`,
		runID, userID, teamArg, teamArg, conversationArg, conversationArg, provider, summary)
	if err != nil {
		cancel()
		return "", nil, nil, err
	}
	s.agentCancels.Store(runID, cancel)
	return runID, runCtx, cancel, nil
}

func (s *Server) finishAgentRunWithSummary(runID, provider, model, status string, summary map[string]any) {
	if runID == "" {
		return
	}
	var modelArg any
	if model != "" {
		modelArg = model
	}
	summaryArg := []byte(`{}`)
	if summary != nil {
		if encoded, err := json.Marshal(summary); err == nil {
			summaryArg = encoded
		}
	}
	// Merge, don't replace: the checkpoint written during the run must survive
	// the final status summary so an interrupted/failed run remains diagnosable.
	_, _ = s.DB.Exec(`UPDATE agent_runs SET model=?,status=?,context_summary=JSON_MERGE_PATCH(context_summary,CAST(? AS JSON)),finished_at=UTC_TIMESTAMP(3)
		WHERE id=UNHEX(?) AND status='running'`, modelArg, status, summaryArg, runID)
	if cancel, ok := s.agentCancels.LoadAndDelete(runID); ok {
		cancel.(context.CancelFunc)()
	}
}

func (s *Server) saveAgentCheckpoint(runID, phase, instruction string, steps []agentToolStep, messages []aiMessage, options map[string]any) {
	if runID == "" {
		return
	}
	tools := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		tools = append(tools, map[string]any{"tool": step.Tool})
	}
	payload, err := json.Marshal(map[string]any{
		"phase":       phase,
		"instruction": truncateRunes(instruction, 2000),
		"steps":       tools,
		"messages":    checkpointMessages(messages),
		"options":     options,
		"updatedAt":   time.Now().UTC(),
	})
	if err != nil {
		return
	}
	if _, err = s.DB.Exec(`UPDATE agent_runs SET context_summary=JSON_SET(context_summary,'$.checkpoint',CAST(? AS JSON))
		WHERE id=UNHEX(?) AND status='running'`, payload, runID); err != nil {
		log.Printf("agent checkpoint save failed run=%s phase=%s err=%v", runID, phase, err)
	}
}

// cancelAgentRun updates the durable state first, then cancels the in-process
// context. The running handler may race to finish; its update is guarded by
// status='running', so a cancelled run is never overwritten back to success.
func (s *Server) cancelAgentRun(ctx context.Context, userID, runID string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE agent_runs SET status='cancelled',finished_at=UTC_TIMESTAMP(3),
		context_summary=JSON_SET(context_summary,'$.cancelled',true,'$.cancelledAt',UTC_TIMESTAMP(3))
		WHERE id=UNHEX(?) AND user_id=UNHEX(?) AND status IN ('running','waiting_approval')`, runID, userID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		var status string
		if err = s.DB.QueryRowContext(ctx, `SELECT status FROM agent_runs WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, runID, userID).Scan(&status); err != nil {
			return bad(404, "RUN_NOT_FOUND", "运行记录不存在")
		}
		return bad(409, "RUN_NOT_RUNNING", "该运行已结束（当前状态："+status+"）")
	}
	if cancel, ok := s.agentCancels.LoadAndDelete(runID); ok {
		cancel.(context.CancelFunc)()
	}
	return nil
}

// recoverStaleAgentRuns runs once at startup. A process restart leaves any
// in-flight run in 'running' forever; mark it interrupted instead of pretending
// it is still working. The 10-minute floor avoids touching a run owned by a
// concurrently starting instance.
func (s *Server) recoverStaleAgentRuns(ctx context.Context) {
	res, err := s.DB.ExecContext(ctx, `UPDATE agent_runs SET status='failed',finished_at=UTC_TIMESTAMP(3),
		context_summary=JSON_SET(context_summary,'$.interrupted',true,'$.interruptedReason','server_restart')
		WHERE status='running' AND started_at < DATE_SUB(UTC_TIMESTAMP(3), INTERVAL 10 MINUTE)`)
	if err != nil {
		log.Printf("agent run recovery failed: %v", err)
		return
	}
	if affected, _ := res.RowsAffected(); affected > 0 {
		log.Printf("agent run recovery marked %d stale run(s) interrupted", affected)
	}
}

// agentRunView is the privacy-safe shape returned by the control API. It never
// includes the original instruction or tool arguments.
func agentRunView(runID, provider, model, status string, summary []byte, startedAt, finishedAt any) map[string]any {
	view := map[string]any{
		"id": runID, "provider": provider, "model": model, "status": status,
		"startedAt": startedAt, "finishedAt": finishedAt,
	}
	var parsed struct {
		Purpose     string `json:"purpose"`
		Interrupted bool   `json:"interrupted"`
		Cancelled   bool   `json:"cancelled"`
		Approval    struct {
			Type     string `json:"type"`
			ID       string `json:"id"`
			Status   string `json:"status"`
			Decision string `json:"decision"`
		} `json:"approval"`
		Checkpoint struct {
			Phase string `json:"phase"`
			Steps []struct {
				Tool string `json:"tool"`
			} `json:"steps"`
		} `json:"checkpoint"`
	}
	if len(summary) > 0 && json.Unmarshal(summary, &parsed) == nil {
		view["purpose"] = parsed.Purpose
		view["interrupted"] = parsed.Interrupted
		view["cancelled"] = parsed.Cancelled
		view["approval"] = parsed.Approval
		view["waitingApproval"] = status == "waiting_approval"
		view["checkpointPhase"] = parsed.Checkpoint.Phase
		tools := make([]string, 0, len(parsed.Checkpoint.Steps))
		for _, step := range parsed.Checkpoint.Steps {
			tools = append(tools, step.Tool)
		}
		view["steps"] = tools
	}
	return view
}

// agentRunSummary is the small, privacy-safe payload attached at the end of a
// run. It never contains instructions, tool arguments, or model output.
func agentRunSummary(purpose string, steps []agentToolStep, extra map[string]any) map[string]any {
	tools := make([]string, 0, len(steps))
	for _, step := range steps {
		tools = append(tools, step.Tool)
	}
	summary := map[string]any{
		"purpose":   purpose,
		"steps":     len(steps),
		"toolCount": len(steps),
		"tools":     tools,
	}
	for key, value := range extra {
		summary[key] = value
	}
	return summary
}

// checkpointMessages keeps enough state to continue after the last completed
// step without replaying tools. Tool payloads are bounded so one large page or
// sandbox output cannot bloat agent_runs.
func checkpointMessages(messages []aiMessage) []aiMessage {
	if len(messages) == 0 {
		return nil
	}
	out := make([]aiMessage, 0, len(messages))
	for _, message := range messages {
		copyMessage := message
		if len(copyMessage.Content) > 16*1024 {
			copyMessage.Content = truncateRunes(copyMessage.Content, 16*1024)
		}
		out = append(out, copyMessage)
	}
	return out
}

// parkAgentRun stops a run at a human decision boundary. The agent has already
// produced its reply and persisted the proposal; the run stays visible as
// waiting_approval until the proposal is decided.
func (s *Server) parkAgentRun(runID, approvalType, approvalID string) error {
	approvalID = strings.ToUpper(approvalID)
	approval, err := json.Marshal(map[string]any{"type": approvalType, "id": approvalID, "status": "pending"})
	if err != nil {
		return err
	}
	res, err := s.DB.Exec(`UPDATE agent_runs SET status='waiting_approval',
		context_summary=JSON_SET(context_summary,'$.checkpoint.phase','waiting_approval','$.approval',CAST(? AS JSON))
		WHERE id=UNHEX(?) AND status='running'`, approval, runID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return bad(409, "RUN_NOT_RUNNING", "运行已结束，无法挂起等待审批")
	}
	if cancel, ok := s.agentCancels.LoadAndDelete(runID); ok {
		cancel.(context.CancelFunc)()
	}
	return nil
}

// resolveApprovalRuns closes the loop when a proposal or team-memory candidate
// is decided. It never blocks the decision itself.
func (s *Server) resolveApprovalRuns(ctx context.Context, approvalType, approvalID, decision string) {
	if _, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO agent_followups(id,run_id,user_id,team_id,conversation_id,approval_type,approval_id,decision)
		SELECT UNHEX(REPLACE(UUID(),'-','')),id,user_id,team_id,conversation_id,?,UNHEX(?),?
		FROM agent_runs WHERE status='waiting_approval'
		AND JSON_UNQUOTE(JSON_EXTRACT(context_summary,'$.approval.type'))=?
		AND UPPER(JSON_UNQUOTE(JSON_EXTRACT(context_summary,'$.approval.id')))=UPPER(?)
		AND conversation_id IS NOT NULL AND user_id IS NOT NULL`, approvalType, approvalID, decision, approvalType, approvalID); err != nil {
		log.Printf("agent followup enqueue failed type=%s id=%s err=%v", approvalType, approvalID, err)
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE agent_runs SET status='succeeded',finished_at=UTC_TIMESTAMP(3),
		context_summary=JSON_SET(context_summary,'$.approval.status',?,'$.approval.decision',?,
		'$.approval.resolvedAt',UTC_TIMESTAMP(3),'$.checkpoint.phase','approval_resolved')
		WHERE status='waiting_approval'
		AND JSON_UNQUOTE(JSON_EXTRACT(context_summary,'$.approval.type'))=?
		AND UPPER(JSON_UNQUOTE(JSON_EXTRACT(context_summary,'$.approval.id')))=UPPER(?)`, decision, decision, approvalType, approvalID)
	if err != nil {
		log.Printf("agent approval resolve failed type=%s id=%s decision=%s err=%v", approvalType, approvalID, decision, err)
	}
}
