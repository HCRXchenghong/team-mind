package live

import (
	"context"
	"log"
	"strings"
	"time"
)

// RunAgentMessageWorker auto-replies once to a human-triggered agent message.
// Auto replies are stored with auto_reply=0 so two assistants cannot loop
// forever; a follow-up requires a new explicit tool call.
func (s *Server) RunAgentMessageWorker(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.processAgentMessages(ctx); err != nil && ctx.Err() == nil {
			log.Printf("agent message worker failed; messages remain unread")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) processAgentMessages(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT HEX(id),HEX(team_id),HEX(from_user_id),HEX(to_user_id),body,COALESCE((SELECT display_name FROM users WHERE id=from_user_id),'')
		FROM agent_messages
		WHERE read_at IS NULL AND auto_reply=1 AND created_at>DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 15 MINUTE)
		ORDER BY created_at LIMIT 5`)
	if err != nil {
		return err
	}
	type message struct {
		id, team, from, to, body, fromName string
	}
	var pending []message
	for rows.Next() {
		var m message
		if err = rows.Scan(&m.id, &m.team, &m.from, &m.to, &m.body, &m.fromName); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, m)
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range pending {
		res, err := s.DB.ExecContext(ctx, `UPDATE agent_messages SET read_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND read_at IS NULL AND auto_reply=1`, m.id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if err = s.autoReplyAgentMessage(ctx, m.id, m.team, m.from, m.to, m.fromName, m.body); err != nil {
			log.Printf("agent auto reply failed for message %s", m.id)
		}
	}
	return nil
}

func (s *Server) autoReplyAgentMessage(ctx context.Context, messageID, teamID, fromUser, toUser, fromName, body string) error {
	allowed, err := s.aiAccessAllowed(ctx, toUser, teamID)
	if err != nil || !allowed {
		return err
	}
	aiCtx := withAITeam(ctx, teamID)
	if !s.aiConfigured(aiCtx) {
		return nil
	}
	messages := []aiMessage{
		{Role: "system", Content: "你是 TeamMind 中接收方成员的 AI 助手。另一名成员的 AI 助手发来了一条协作消息。请用简洁中文回复，确认你理解的事项、需要对方补充的信息或下一步；不要调用工具，不要声称已修改任何数据。控制在 300 字以内。"},
		{Role: "user", Content: "来自 " + fromName + " 的 AI 消息：\n" + truncateRunes(body, 3000)},
	}
	reply, _, err := s.replyMessages(aiCtx, messages)
	if err != nil {
		return err
	}
	reply = strings.TrimSpace(truncateRunes(reply, 2000))
	if reply == "" {
		return nil
	}
	replyID := id()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_messages(id,team_id,from_user_id,to_user_id,body,auto_reply,parent_message_id) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,0,UNHEX(?))`, replyID, teamID, toUser, fromUser, reply, messageID); err != nil {
		return err
	}
	if err = notifyResource(tx, teamID, fromUser, "agent_message", "AI 助手回复", reply, "agent_message", replyID); err != nil {
		return err
	}
	return tx.Commit()
}
