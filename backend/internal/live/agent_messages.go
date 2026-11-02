package live

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type agentMessageView struct {
	ID         string     `json:"id"`
	TeamID     string     `json:"teamId"`
	FromUserID string     `json:"fromUserId"`
	FromName   string     `json:"fromName"`
	ToUserID   string     `json:"toUserId"`
	Body       string     `json:"body"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReadAt     *time.Time `json:"readAt,omitempty"`
}

func (s *Server) agentMessages(w http.ResponseWriter, r *http.Request, u User, path string) error {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 2 && r.Method == http.MethodGet {
		rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(m.id),HEX(m.team_id),HEX(m.from_user_id),COALESCE(u.display_name,''),HEX(m.to_user_id),m.body,m.created_at,m.read_at
			FROM agent_messages m LEFT JOIN users u ON u.id=m.from_user_id
			WHERE m.to_user_id=UNHEX(?) ORDER BY m.created_at DESC LIMIT 50`, u.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := []agentMessageView{}
		for rows.Next() {
			var item agentMessageView
			var read sql.NullTime
			if err = rows.Scan(&item.ID, &item.TeamID, &item.FromUserID, &item.FromName, &item.ToUserID, &item.Body, &item.CreatedAt, &read); err != nil {
				return err
			}
			if read.Valid {
				value := read.Time
				item.ReadAt = &value
			}
			out = append(out, item)
		}
		write(w, http.StatusOK, out)
		return rows.Err()
	}
	if len(parts) == 3 && r.Method == http.MethodPatch && validID(parts[2]) {
		res, err := s.DB.ExecContext(r.Context(), `UPDATE agent_messages SET read_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND to_user_id=UNHEX(?) AND read_at IS NULL`, parts[2], u.ID)
		if err != nil {
			return err
		}
		changed, _ := res.RowsAffected()
		if changed == 0 {
			return bad(http.StatusNotFound, "NOT_FOUND", "消息不存在或已读")
		}
		write(w, http.StatusOK, map[string]bool{"read": true})
		return nil
	}
	return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
}

func (s *Server) agentToolSendAgentMessage(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "发送 Agent 消息需要团队上下文")
	}
	var args struct {
		ToUserID       string `json:"toUserId"`
		ToDepartmentID string `json:"toDepartmentId"`
		Body           string `json:"message"`
	}
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "Agent 消息参数无效")
	}
	args.ToUserID = strings.ToUpper(strings.TrimSpace(args.ToUserID))
	args.ToDepartmentID = strings.ToUpper(strings.TrimSpace(args.ToDepartmentID))
	args.Body = strings.TrimSpace(args.Body)
	if args.Body == "" || len([]rune(args.Body)) > 4000 {
		return nil, nil, "not_required", bad(http.StatusBadRequest, "VALIDATION_ERROR", "消息内容不能为空且不超过 4000 字")
	}
	if (args.ToUserID == "") == (args.ToDepartmentID == "") {
		return nil, nil, "not_required", bad(http.StatusBadRequest, "VALIDATION_ERROR", "请指定接收成员或部门/小组")
	}
	tx, err := s.DB.BeginTx(tc.ctx, nil)
	if err != nil {
		return nil, nil, "not_required", err
	}
	defer tx.Rollback()
	if _, err = activeTeamRoleTx(tc.ctx, tx, tc.teamID, tc.user); err != nil {
		return nil, nil, "not_required", err
	}
	recipient := args.ToUserID
	targetLabel := "成员"
	if args.ToDepartmentID != "" {
		if !validID(args.ToDepartmentID) {
			return nil, nil, "not_required", bad(http.StatusBadRequest, "INVALID_ID", "部门/小组 ID 无效")
		}
		if err = tx.QueryRowContext(tc.ctx, `SELECT COALESCE(HEX(lead_user_id),'') FROM departments WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, args.ToDepartmentID, tc.teamID).Scan(&recipient); err != nil {
			return nil, nil, "not_required", err
		}
		if recipient == "" {
			return nil, nil, "not_required", bad(http.StatusConflict, "NO_UNIT_LEAD", "该部门/小组还没有负责人")
		}
		targetLabel = "部门/小组负责人"
	}
	if !validID(recipient) || recipient == tc.user.ID {
		return nil, nil, "not_required", bad(http.StatusBadRequest, "VALIDATION_ERROR", "不能给自己发送 Agent 消息")
	}
	var recipientName string
	if err = tx.QueryRowContext(tc.ctx, `SELECT u.display_name FROM users u JOIN team_memberships m ON m.user_id=u.id WHERE u.id=UNHEX(?) AND m.team_id=UNHEX(?) AND m.status='active' AND u.status='active'`, recipient, tc.teamID).Scan(&recipientName); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, "not_required", bad(http.StatusNotFound, "RECIPIENT_NOT_FOUND", "接收成员不在当前团队")
		}
		return nil, nil, "not_required", err
	}
	var senderName string
	_ = tx.QueryRowContext(tc.ctx, `SELECT display_name FROM users WHERE id=UNHEX(?)`, tc.user.ID).Scan(&senderName)
	messageID := id()
	var conversation any
	if validID(tc.conversationID) {
		conversation = tc.conversationID
	}
	if _, err = tx.ExecContext(tc.ctx, `INSERT INTO agent_messages(id,team_id,from_user_id,to_user_id,from_conversation_id,body) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),?)`, messageID, tc.teamID, tc.user.ID, recipient, conversation, conversation, args.Body); err != nil {
		return nil, nil, "not_required", err
	}
	title := "来自 " + senderName + " 的 AI 消息"
	if err = notifyResource(tx, tc.teamID, recipient, "agent_message", title, args.Body, "agent_message", messageID); err != nil {
		return nil, nil, "not_required", err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, "not_required", err
	}
	result := map[string]any{"messageId": messageID, "toUserId": recipient, "toName": recipientName, "target": targetLabel}
	raw, _ := json.Marshal(result)
	return result, raw, "not_required", nil
}

func (s *Server) agentToolListAgentMessages(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "读取 Agent 消息需要团队上下文")
	}
	rows, err := s.DB.QueryContext(tc.ctx, `SELECT HEX(m.id),HEX(m.from_user_id),COALESCE(u.display_name,''),m.body,m.created_at,m.read_at
		FROM agent_messages m LEFT JOIN users u ON u.id=m.from_user_id
		WHERE m.to_user_id=UNHEX(?) AND m.team_id=UNHEX(?) ORDER BY m.created_at DESC LIMIT 10`, tc.user.ID, tc.teamID)
	if err != nil {
		return nil, nil, "not_required", err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, from, name, body string
		var created time.Time
		var read sql.NullTime
		if err = rows.Scan(&id, &from, &name, &body, &created, &read); err != nil {
			return nil, nil, "not_required", err
		}
		items = append(items, map[string]any{"id": id, "fromUserId": from, "fromName": name, "message": body, "createdAt": created, "read": read.Valid})
	}
	result := map[string]any{"messages": items, "count": len(items)}
	raw, _ := json.Marshal(result)
	return result, raw, "not_required", rows.Err()
}
