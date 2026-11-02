package live

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// RunApprovalFollowupWorker turns an approval decision into a durable
// follow-up turn. It is deliberately separate from the decision transaction:
// a slow model can never block or fail the approval itself, and a crash leaves
// a retryable row instead of losing the follow-up.
func (s *Server) RunApprovalFollowupWorker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		s.processApprovalFollowups(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) processApprovalFollowups(ctx context.Context) {
	rows, err := s.DB.QueryContext(ctx, `SELECT HEX(id) FROM agent_followups
		WHERE status='pending' AND next_attempt_at<=UTC_TIMESTAMP(3)
		ORDER BY created_at LIMIT 5`)
	if err != nil {
		log.Printf("approval followup scan failed: %v", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		res, err := s.DB.ExecContext(ctx, `UPDATE agent_followups SET status='processing',attempts=attempts+1
			WHERE id=UNHEX(?) AND status='pending'`, id)
		if err != nil {
			continue
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			continue
		}
		if err = s.runApprovalFollowup(ctx, id); err != nil {
			s.retryApprovalFollowup(ctx, id, err)
		}
	}
}

type approvalFollowupRow struct {
	ID             string
	RunID          string
	UserID         string
	TeamID         string
	ConversationID string
	ApprovalID     string
	ApprovalType   string
	Decision       string
	Attempts       int
	Summary        []byte
}

func (s *Server) runApprovalFollowup(ctx context.Context, id string) error {
	var row approvalFollowupRow
	err := s.DB.QueryRowContext(ctx, `SELECT HEX(f.id),HEX(f.run_id),HEX(f.user_id),COALESCE(HEX(f.team_id),''),
		HEX(f.conversation_id),HEX(f.approval_id),f.approval_type,f.decision,f.attempts,r.context_summary
		FROM agent_followups f JOIN agent_runs r ON r.id=f.run_id WHERE f.id=UNHEX(?)`, id).Scan(
		&row.ID, &row.RunID, &row.UserID, &row.TeamID, &row.ConversationID, &row.ApprovalID, &row.ApprovalType, &row.Decision, &row.Attempts, &row.Summary)
	if err != nil {
		return err
	}
	ctx = withAITeam(ctx, row.TeamID)
	var state struct {
		Checkpoint struct {
			Messages []aiMessage `json:"messages"`
		} `json:"checkpoint"`
	}
	_ = json.Unmarshal(row.Summary, &state)
	prior := make([]aiMessage, 0, 12)
	for _, message := range state.Checkpoint.Messages {
		if message.Role != "user" && message.Role != "assistant" || strings.TrimSpace(message.Content) == "" {
			continue
		}
		if len([]rune(message.Content)) > 4000 {
			message.Content = truncateRunes(message.Content, 4000)
		}
		prior = append(prior, message)
	}
	if len(prior) > 12 {
		prior = prior[len(prior)-12:]
	}
	messages := []aiMessage{{
		Role:    "system",
		Content: "你是 TeamMind 的项目助理。用户刚处理了一条审批。请用中文简短确认审批结果、说明对正式数据的影响，以及用户下一步可以去哪里查看。不要声称执行了没有发生的写操作，也不要再次提交提案。",
	}}
	messages = append(messages, prior...)
	messages = append(messages, aiMessage{
		Role:    "user",
		Content: fmt.Sprintf("系统事件：审批类型 %s，结果是「%s」。请直接给用户一条确认回复。", row.ApprovalType, decisionText(row.Decision)),
	})
	var content strings.Builder
	model, err := s.streamAIReply(ctx, messages, func(delta string) error {
		content.WriteString(delta)
		return nil
	})
	if err != nil {
		return err
	}
	text := strings.TrimSpace(content.String())
	if text == "" {
		return fmt.Errorf("empty followup response")
	}
	if err = s.saveApprovalFollowup(ctx, row, truncateRunes(text, 8000), model); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE agent_followups SET status='succeeded',last_error='' WHERE id=UNHEX(?)`, id)
	return err
}

func (s *Server) saveApprovalFollowup(ctx context.Context, row approvalFollowupRow, content, model string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner string
	if err = tx.QueryRowContext(ctx, `SELECT HEX(owner_user_id) FROM conversations WHERE id=UNHEX(?) FOR UPDATE`, row.ConversationID).Scan(&owner); err != nil {
		return err
	}
	if !strings.EqualFold(owner, row.UserID) {
		return bad(403, "CONVERSATION_FORBIDDEN", "对话不属于该用户")
	}
	var created time.Time
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(DATE_ADD(MAX(created_at),INTERVAL 1000 MICROSECOND),UTC_TIMESTAMP(3)) FROM conversation_messages WHERE conversation_id=UNHEX(?)`, row.ConversationID).Scan(&created); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO conversation_messages(id,conversation_id,role,content,provider,model,created_at)
		VALUES(UNHEX(?),UNHEX(?),'assistant',?,'provider_pool',?,?)`, id(), row.ConversationID, content, model, created); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE conversations SET updated_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?)`, row.ConversationID); err != nil {
		return err
	}
	summary, _ := json.Marshal(map[string]any{
		"purpose": "approval_followup", "approvalType": row.ApprovalType,
		"approvalId": row.ApprovalID, "decision": row.Decision, "parentRun": row.RunID,
	})
	var teamArg any
	if row.TeamID != "" {
		teamArg = row.TeamID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_runs(id,user_id,team_id,conversation_id,provider,model,status,context_summary,started_at,finished_at)
		VALUES(UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),UNHEX(?),'provider_pool',?,'succeeded',?,UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`,
		id(), row.UserID, teamArg, teamArg, row.ConversationID, model, summary); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) retryApprovalFollowup(ctx context.Context, id string, cause error) {
	message := truncateRunes(cause.Error(), 480)
	_, _ = s.DB.ExecContext(ctx, `UPDATE agent_followups SET
		status=IF(attempts>=5,'failed','pending'),
		last_error=?,
		next_attempt_at=DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 30 SECOND)
		WHERE id=UNHEX(?)`, message, id)
}
