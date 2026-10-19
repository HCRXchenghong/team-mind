package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

type reminderPayload struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	ResourceType string `json:"resourceType,omitempty"`
	ResourceID   string `json:"resourceId,omitempty"`
}

type reminder struct {
	ID         string    `json:"id"`
	TeamID     string    `json:"teamId"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	RunAt      time.Time `json:"runAt"`
	Status     string    `json:"status"`
	Attempts   uint32    `json:"attempts"`
	CreatedAt  time.Time `json:"createdAt"`
	Resource   string    `json:"resourceType,omitempty"`
	ResourceID string    `json:"resourceId,omitempty"`
}

// reminders exposes only jobs owned by the authenticated user. Team membership
// is checked again by the worker at delivery time so a removed member cannot
// receive a reminder that continues to reveal team data.
func (s *Server) reminders(w http.ResponseWriter, r *http.Request, u User, path string) error {
	parts := strings.Split(path, "/")
	if len(parts) == 2 && r.Method == http.MethodGet {
		rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(id),COALESCE(HEX(team_id),''),payload,run_at,status,attempts,created_at FROM scheduled_jobs WHERE owner_user_id=UNHEX(?) AND job_type='notification.reminder' ORDER BY run_at DESC LIMIT 200`, u.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := make([]reminder, 0)
		for rows.Next() {
			var item reminder
			var raw []byte
			if err := rows.Scan(&item.ID, &item.TeamID, &raw, &item.RunAt, &item.Status, &item.Attempts, &item.CreatedAt); err != nil {
				return err
			}
			var payload reminderPayload
			if err := json.Unmarshal(raw, &payload); err != nil {
				return errors.New("invalid reminder payload stored in database")
			}
			item.Title, item.Body, item.Resource, item.ResourceID = payload.Title, payload.Body, payload.ResourceType, payload.ResourceID
			out = append(out, item)
		}
		write(w, http.StatusOK, out)
		return rows.Err()
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		var input struct {
			TeamID       string `json:"teamId"`
			Title        string `json:"title"`
			Body         string `json:"body"`
			RunAt        string `json:"runAt"`
			ResourceType string `json:"resourceType"`
			ResourceID   string `json:"resourceId"`
		}
		if err := body(w, r, &input); err != nil {
			return err
		}
		input.Title = strings.TrimSpace(input.Title)
		input.Body = strings.TrimSpace(input.Body)
		if input.Title == "" || len([]rune(input.Title)) > 160 || len([]rune(input.Body)) > 2000 {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "提醒标题为必填项且不超过 160 字，内容不能超过 2000 字")
		}
		runAt, err := time.Parse(time.RFC3339, input.RunAt)
		if err != nil {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "提醒时间必须包含时区")
		}
		runAt = runAt.UTC()
		now := time.Now().UTC()
		if runAt.Before(now.Add(time.Minute)) || runAt.After(now.AddDate(1, 0, 0)) {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "提醒时间需在 1 分钟后至 1 年内")
		}
		if input.TeamID != "" && !validID(input.TeamID) {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "团队无效")
		}
		if (input.ResourceType == "") != (input.ResourceID == "") || input.ResourceType != "" && (input.ResourceType != "task" || !validID(input.ResourceID) || input.TeamID == "") {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "关联资源必须是当前团队中的任务")
		}
		if input.TeamID != "" {
			var member int
			err = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=UNHEX(?) AND m.user_id=UNHEX(?) AND m.status='active' AND t.status='active'`, input.TeamID, u.ID).Scan(&member)
			if err != nil {
				return err
			}
			if member != 1 {
				return bad(http.StatusForbidden, "FORBIDDEN", "你已无法访问该团队")
			}
		}
		if input.ResourceType == "task" {
			var exists int
			err = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM tasks WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, input.ResourceID, input.TeamID).Scan(&exists)
			if err != nil {
				return err
			}
			if exists != 1 {
				return bad(http.StatusNotFound, "NOT_FOUND", "任务不存在")
			}
		}
		payload, err := json.Marshal(reminderPayload{Title: input.Title, Body: input.Body, ResourceType: input.ResourceType, ResourceID: input.ResourceID})
		if err != nil {
			return err
		}
		jid := id()
		var teamArg any
		if input.TeamID != "" {
			teamArg = input.TeamID
		}
		_, err = s.DB.ExecContext(r.Context(), `INSERT INTO scheduled_jobs(id,team_id,owner_user_id,job_type,payload,run_at,dedupe_key) VALUES(UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),UNHEX(?),'notification.reminder',?,?,?)`, jid, teamArg, teamArg, u.ID, payload, runAt, "reminder:"+jid)
		if err != nil {
			return err
		}
		write(w, http.StatusCreated, map[string]string{"id": jid})
		return nil
	}
	if len(parts) == 3 && r.Method == http.MethodDelete {
		if !validID(parts[2]) {
			return bad(http.StatusNotFound, "NOT_FOUND", "提醒不存在")
		}
		result, err := s.DB.ExecContext(r.Context(), `UPDATE scheduled_jobs SET status='cancelled' WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?) AND job_type='notification.reminder' AND status='scheduled'`, parts[2], u.ID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 0 {
			return bad(http.StatusConflict, "REMINDER_NOT_CANCELLABLE", "提醒已执行、已取消或不存在")
		}
		write(w, http.StatusOK, map[string]bool{"cancelled": true})
		return nil
	}
	return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}

// RunReminderWorker is safe to run in more than one API process. Each due job
// is locked and committed with its in-app notification in one transaction;
// after a process restart, uncommitted work remains scheduled for retry.
func (s *Server) RunReminderWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastErrorLog := time.Time{}
	for {
		if err := s.processDueReminders(ctx); err != nil && ctx.Err() == nil {
			// Avoid including database arguments or reminder contents in logs and
			// keep a persistent database outage from flooding the service logs.
			if time.Since(lastErrorLog) >= 30*time.Second {
				log.Printf("reminder worker tick failed; will retry")
				lastErrorLog = time.Now()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) processDueReminders(ctx context.Context) error {
	for i := 0; i < 25; i++ {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var jobID, ownerID, teamID string
		var raw []byte
		err = tx.QueryRowContext(ctx, `SELECT HEX(id),HEX(owner_user_id),COALESCE(HEX(team_id),''),payload FROM scheduled_jobs WHERE job_type='notification.reminder' AND status='scheduled' AND run_at<=UTC_TIMESTAMP(3) ORDER BY run_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&jobID, &ownerID, &teamID, &raw)
		if err == sql.ErrNoRows {
			_ = tx.Rollback()
			return nil
		}
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		var payload reminderPayload
		if err = json.Unmarshal(raw, &payload); err != nil || payload.Title == "" {
			_, err = tx.ExecContext(ctx, `UPDATE scheduled_jobs SET status='failed',attempts=attempts+1 WHERE id=UNHEX(?)`, jobID)
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
			if err != nil {
				return err
			}
			continue
		}
		var active int
		if teamID == "" {
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=UNHEX(?) AND status='active'`, ownerID).Scan(&active)
		} else {
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u JOIN team_memberships m ON m.user_id=u.id JOIN teams t ON t.id=m.team_id WHERE u.id=UNHEX(?) AND u.status='active' AND m.team_id=UNHEX(?) AND m.status='active' AND t.status='active'`, ownerID, teamID).Scan(&active)
		}
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if active == 1 {
			resourceType, resourceID := "scheduled_reminder", jobID
			if payload.ResourceType == "task" && validID(payload.ResourceID) && teamID != "" {
				var taskExists int
				if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL`, payload.ResourceID, teamID).Scan(&taskExists); err != nil {
					_ = tx.Rollback()
					return err
				}
				if taskExists == 1 {
					resourceType, resourceID = "task", payload.ResourceID
				}
			}
			if err = notifyResource(tx, teamID, ownerID, "scheduled_reminder", payload.Title, payload.Body, resourceType, resourceID); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		status := "succeeded"
		if active != 1 {
			status = "cancelled"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE scheduled_jobs SET status=?,attempts=attempts+1 WHERE id=UNHEX(?) AND status='scheduled'`, status, jobID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
