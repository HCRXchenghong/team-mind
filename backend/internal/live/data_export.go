package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// exportPersonalData returns account-owned data only. Team-wide resources and
// reusable secrets are deliberately excluded from the export.
func (s *Server) exportPersonalData(w http.ResponseWriter, r *http.Request, u User) error {
	if err := method(r, http.MethodGet); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	data := map[string]any{
		"schemaVersion": 1,
		"exportedAt":    time.Now().UTC(),
		"scope":         "导出当前账号拥有的数据和直接关联记录；团队共享业务数据不包含在内。密码哈希、访问令牌、微信渠道凭据及推送订阅密钥不会导出。",
		"account": map[string]any{
			"id": u.ID, "email": u.Email, "name": u.Name, "timezone": u.Timezone,
		},
	}
	var accountCreatedAt time.Time
	if err = tx.QueryRowContext(r.Context(), `SELECT created_at FROM users WHERE id=UNHEX(?)`, u.ID).Scan(&accountCreatedAt); err != nil {
		return err
	}
	data["account"].(map[string]any)["createdAt"] = accountCreatedAt
	queries := []struct {
		key   string
		query string
	}{
		{"teamMemberships", `SELECT HEX(m.team_id) AS teamId,t.name AS teamName,m.role,m.status,m.joined_at AS joinedAt FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.user_id=UNHEX(?) ORDER BY m.joined_at,m.team_id`},
		{"personalMemories", `SELECT HEX(id) AS id,category,content,confidence,importance,sensitivity,confirmed_at AS confirmedAt,review_at AS reviewAt,expires_at AS expiresAt,created_at AS createdAt,updated_at AS updatedAt FROM user_memories WHERE owner_user_id=UNHEX(?) AND deleted_at IS NULL ORDER BY created_at,id`},
		{"conversations", `SELECT HEX(c.id) AS id,COALESCE(HEX(c.team_id),'') AS teamId,c.visibility,c.title,c.created_at AS createdAt,c.updated_at AS updatedAt FROM conversations c WHERE c.owner_user_id=UNHEX(?) AND (c.team_id IS NULL OR EXISTS(SELECT 1 FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=c.team_id AND m.user_id=c.owner_user_id AND m.status='active' AND t.status='active')) ORDER BY c.created_at,c.id`},
		{"conversationMessages", `SELECT HEX(m.id) AS id,HEX(m.conversation_id) AS conversationId,m.role,m.content,m.provider,m.model,m.token_usage AS tokenUsage,m.created_at AS createdAt FROM conversation_messages m JOIN conversations c ON c.id=m.conversation_id WHERE c.owner_user_id=UNHEX(?) AND (c.team_id IS NULL OR EXISTS(SELECT 1 FROM team_memberships tm JOIN teams t ON t.id=tm.team_id WHERE tm.team_id=c.team_id AND tm.user_id=c.owner_user_id AND tm.status='active' AND t.status='active')) ORDER BY m.conversation_id,m.created_at,m.id`},
		{"notifications", `SELECT HEX(id) AS id,COALESCE(HEX(team_id),'') AS teamId,type,title,body,resource_type AS resourceType,COALESCE(HEX(resource_id),'') AS resourceId,read_at AS readAt,created_at AS createdAt FROM notifications WHERE recipient_user_id=UNHEX(?) ORDER BY created_at,id`},
		{"notificationPreferences", `SELECT enabled,email_enabled AS emailEnabled,push_enabled AS pushEnabled,native_push_enabled AS nativePushEnabled,quiet_start AS quietStart,quiet_end AS quietEnd,updated_at AS updatedAt FROM notification_preferences WHERE user_id=UNHEX(?)`},
		{"nativeNotificationDevices", `SELECT HEX(id) AS id,platform,status,created_at AS createdAt,updated_at AS updatedAt FROM notification_native_devices WHERE user_id=UNHEX(?) ORDER BY created_at,id`},
		{"reminders", `SELECT HEX(id) AS id,COALESCE(HEX(team_id),'') AS teamId,job_type AS jobType,payload,run_at AS runAt,status,attempts,created_at AS createdAt FROM scheduled_jobs WHERE owner_user_id=UNHEX(?) AND job_type='notification.reminder' ORDER BY created_at,id`},
		{"agentRuns", `SELECT HEX(id) AS id,COALESCE(HEX(team_id),'') AS teamId,COALESCE(HEX(conversation_id),'') AS conversationId,provider,model,status,context_summary AS contextSummary,started_at AS startedAt,finished_at AS finishedAt,created_at AS createdAt FROM agent_runs a WHERE user_id=UNHEX(?) AND (team_id IS NULL OR EXISTS(SELECT 1 FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=a.team_id AND m.user_id=a.user_id AND m.status='active' AND t.status='active')) ORDER BY created_at,id`},
		{"agentToolCalls", `SELECT HEX(id) AS id,HEX(run_id) AS runId,COALESCE(HEX(team_id),'') AS teamId,tool_name AS toolName,argument_summary AS argumentSummary,result_summary AS resultSummary,approval_status AS approvalStatus,status,created_at AS createdAt FROM agent_tool_calls c WHERE user_id=UNHEX(?) AND (team_id IS NULL OR EXISTS(SELECT 1 FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=c.team_id AND m.user_id=c.user_id AND m.status='active' AND t.status='active')) ORDER BY created_at,id`},
		{"notificationDeliveries", `SELECT HEX(id) AS id,COALESCE(HEX(notification_id),'') AS notificationId,notification_type AS notificationType,channel,status,attempts,last_error AS lastError,created_at AS createdAt,delivered_at AS deliveredAt FROM notification_deliveries WHERE recipient_user_id=UNHEX(?) ORDER BY created_at,id`},
		{"wechatBindings", `SELECT HEX(c.id) AS channelAccountId,c.provider,c.status,b.bound_at AS boundAt,b.unbound_at AS unboundAt FROM channel_bindings b JOIN channel_accounts c ON c.id=b.channel_account_id WHERE b.user_id=UNHEX(?) ORDER BY b.bound_at`},
		{"sessions", `SELECT HEX(id) AS id,expires_at AS expiresAt,revoked_at AS revokedAt,created_at AS createdAt FROM auth_sessions WHERE user_id=UNHEX(?) ORDER BY created_at`},
		{"passwordResetRequests", `SELECT expires_at AS expiresAt,used_at AS usedAt,created_at AS createdAt FROM password_resets WHERE user_id=UNHEX(?) ORDER BY created_at`},
	}
	for _, item := range queries {
		rows, queryErr := exportRows(r.Context(), tx, item.query, u.ID)
		if queryErr != nil {
			return queryErr
		}
		data[item.key] = rows
	}
	workspaceFiles, err := exportAgentWorkspace(r.Context(), tx, u.ID)
	if err != nil {
		return err
	}
	data["personalAgentWorkspaceFiles"] = workspaceFiles
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_logs(actor_user_id,action,resource_type,after_summary) VALUES(UNHEX(?),'user.data_exported','personal_data_export',JSON_OBJECT('format','json','schemaVersion',1))`, u.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", `attachment; filename="teammind-personal-data.json"`)
	write(w, http.StatusOK, data)
	return nil
}

func exportRows(ctx context.Context, tx *sql.Tx, query string, userID string) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err = rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			value := values[i]
			if raw, ok := value.([]byte); ok {
				if types[i].DatabaseTypeName() == "JSON" {
					var decoded any
					if json.Unmarshal(raw, &decoded) == nil {
						value = decoded
					} else {
						value = string(raw)
					}
				} else {
					value = string(raw)
				}
			}
			row[column] = value
		}
		result = append(result, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
