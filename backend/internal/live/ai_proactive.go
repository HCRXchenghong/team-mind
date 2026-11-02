package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// aiProactivePayload is stored in scheduled_jobs.payload for job_type
// "ai.proactive". The row's run_at is always the next execution time.
type aiProactivePayload struct {
	Kind       string `json:"kind"`     // greeting | daily_report
	Cadence    string `json:"cadence"`  // daily | weekdays | weekly | once
	Time       string `json:"time"`     // HH:MM in Timezone
	Weekday    int    `json:"weekday"`  // ISO 1=Monday .. 7=Sunday, for weekly
	Timezone   string `json:"timezone"` // IANA
	Prompt     string `json:"prompt"`   // optional extra instruction
	Enabled    bool   `json:"enabled"`  // false keeps the job paused
	LastRunAt  string `json:"lastRunAt,omitempty"`
	LastStatus string `json:"lastStatus,omitempty"` // ai | template | failed
	LastError  string `json:"lastError,omitempty"`
	LastTitle  string `json:"lastTitle,omitempty"`
	LastBody   string `json:"lastBody,omitempty"`
}

type aiProactiveInput struct {
	Kind     string `json:"kind"`
	Cadence  string `json:"cadence"`
	Time     string `json:"time"`
	Weekday  int    `json:"weekday"`
	Timezone string `json:"timezone"`
	Prompt   string `json:"prompt"`
	RunAt    string `json:"runAt"`
	TeamID   string `json:"teamId"`
	Enabled  *bool  `json:"enabled"`
}

type aiProactiveJob struct {
	ID         string    `json:"id"`
	TeamID     string    `json:"teamId"`
	Status     string    `json:"status"`
	Attempts   uint32    `json:"attempts"`
	NextRunAt  time.Time `json:"nextRunAt"`
	CreatedAt  time.Time `json:"createdAt"`
	Kind       string    `json:"kind"`
	Cadence    string    `json:"cadence"`
	Time       string    `json:"time"`
	Weekday    int       `json:"weekday"`
	Timezone   string    `json:"timezone"`
	Prompt     string    `json:"prompt"`
	Enabled    bool      `json:"enabled"`
	LastRunAt  string    `json:"lastRunAt"`
	LastStatus string    `json:"lastStatus"`
	LastError  string    `json:"lastError"`
	LastTitle  string    `json:"lastTitle"`
	LastBody   string    `json:"lastBody"`
}

// aiProactive exposes personal recurring AI greetings/daily reports.
func (s *Server) aiProactive(w http.ResponseWriter, r *http.Request, u User, path string) error {
	parts := strings.Split(path, "/")
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			return s.listAIProactive(w, r, u)
		case http.MethodPost:
			return s.createAIProactive(w, r, u)
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持的请求方法")
		}
	}
	if len(parts) == 3 && validID(parts[2]) {
		switch r.Method {
		case http.MethodPatch:
			return s.updateAIProactive(w, r, u, parts[2])
		case http.MethodDelete:
			return s.deleteAIProactive(w, r, u, parts[2])
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持的请求方法")
		}
	}
	if len(parts) == 4 && validID(parts[2]) && parts[3] == "run" && r.Method == http.MethodPost {
		return s.runAIProactiveNow(w, r, u, parts[2])
	}
	return bad(http.StatusNotFound, "NOT_FOUND", "接口不存在")
}

func (s *Server) listAIProactive(w http.ResponseWriter, r *http.Request, u User) error {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(id),COALESCE(HEX(team_id),''),payload,run_at,status,attempts,created_at
		FROM scheduled_jobs WHERE owner_user_id=UNHEX(?) AND job_type='ai.proactive' AND status<>'cancelled'
		ORDER BY created_at DESC LIMIT 50`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := make([]aiProactiveJob, 0)
	for rows.Next() {
		var job aiProactiveJob
		var raw []byte
		if err = rows.Scan(&job.ID, &job.TeamID, &raw, &job.NextRunAt, &job.Status, &job.Attempts, &job.CreatedAt); err != nil {
			return err
		}
		var p aiProactivePayload
		if err = json.Unmarshal(raw, &p); err != nil {
			return errors.New("invalid ai proactive payload stored in database")
		}
		job.Kind, job.Cadence, job.Time, job.Weekday = p.Kind, p.Cadence, p.Time, p.Weekday
		job.Timezone, job.Prompt, job.Enabled = p.Timezone, p.Prompt, p.Enabled
		job.LastRunAt, job.LastStatus, job.LastError = p.LastRunAt, p.LastStatus, p.LastError
		job.LastTitle, job.LastBody = p.LastTitle, p.LastBody
		out = append(out, job)
	}
	write(w, http.StatusOK, out)
	return rows.Err()
}

func (s *Server) createAIProactive(w http.ResponseWriter, r *http.Request, u User) error {
	var x aiProactiveInput
	if err := body(w, r, &x); err != nil {
		return err
	}
	if x.TeamID == "" || !validID(x.TeamID) {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请选择组织")
	}
	var member int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=UNHEX(?) AND m.user_id=UNHEX(?) AND m.status='active' AND t.status='active'`, x.TeamID, u.ID).Scan(&member); err != nil {
		return err
	}
	if member != 1 {
		return bad(http.StatusForbidden, "FORBIDDEN", "你已无法访问该组织")
	}
	var active int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM scheduled_jobs WHERE owner_user_id=UNHEX(?) AND job_type='ai.proactive' AND status IN ('scheduled','paused','running')`, u.ID).Scan(&active); err != nil {
		return err
	}
	if active >= 10 {
		return bad(http.StatusConflict, "LIMIT_REACHED", "最多同时保留 10 个 AI 定时计划")
	}
	payload, runAt, e := normalizeAIProactive(u, x)
	if e != nil {
		return e
	}
	jobID := id()
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	status := "scheduled"
	if !payload.Enabled {
		status = "paused"
	}
	if _, err = s.DB.ExecContext(r.Context(), `INSERT INTO scheduled_jobs(id,team_id,owner_user_id,job_type,payload,run_at,status,dedupe_key) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'ai.proactive',?,?,?,?)`, jobID, x.TeamID, u.ID, raw, runAt, status, "ai-proactive:"+jobID); err != nil {
		return err
	}
	write(w, http.StatusCreated, map[string]any{"id": jobID, "nextRunAt": runAt})
	return nil
}

func (s *Server) updateAIProactive(w http.ResponseWriter, r *http.Request, u User, jobID string) error {
	var x aiProactiveInput
	if err := body(w, r, &x); err != nil {
		return err
	}
	var raw []byte
	var teamID, status string
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COALESCE(HEX(team_id),''),payload,status FROM scheduled_jobs WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?) AND job_type='ai.proactive' AND status<>'cancelled'`, jobID, u.ID).Scan(&teamID, &raw, &status); err == sql.ErrNoRows {
		return bad(http.StatusNotFound, "NOT_FOUND", "计划不存在")
	} else if err != nil {
		return err
	}
	if status == "running" {
		return bad(http.StatusConflict, "JOB_RUNNING", "计划正在生成中，请稍后再试")
	}
	var current aiProactivePayload
	if err := json.Unmarshal(raw, &current); err != nil {
		return errors.New("invalid ai proactive payload stored in database")
	}
	if x.TeamID == "" {
		x.TeamID = teamID
	}
	if x.TeamID != teamID {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "计划所属组织不可修改")
	}
	payload, runAt, e := normalizeAIProactive(u, x)
	if e != nil {
		return e
	}
	payload.LastRunAt, payload.LastStatus, payload.LastError = current.LastRunAt, current.LastStatus, current.LastError
	payload.LastTitle, payload.LastBody = current.LastTitle, current.LastBody
	newStatus := "scheduled"
	if !payload.Enabled {
		newStatus = "paused"
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err = s.DB.ExecContext(r.Context(), `UPDATE scheduled_jobs SET payload=?,run_at=?,status=?,attempts=0 WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?) AND job_type='ai.proactive'`, encoded, runAt, newStatus, jobID, u.ID); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]any{"saved": true, "enabled": payload.Enabled, "nextRunAt": runAt})
	return nil
}

func (s *Server) deleteAIProactive(w http.ResponseWriter, r *http.Request, u User, jobID string) error {
	result, err := s.DB.ExecContext(r.Context(), `UPDATE scheduled_jobs SET status='cancelled' WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?) AND job_type='ai.proactive' AND status IN ('scheduled','paused','failed','succeeded')`, jobID, u.ID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return bad(http.StatusNotFound, "NOT_FOUND", "计划不存在或正在运行")
	}
	write(w, http.StatusOK, map[string]bool{"deleted": true})
	return nil
}

func (s *Server) runAIProactiveNow(w http.ResponseWriter, r *http.Request, u User, jobID string) error {
	var raw []byte
	var status string
	if err := s.DB.QueryRowContext(r.Context(), `SELECT payload,status FROM scheduled_jobs WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?) AND job_type='ai.proactive' AND status<>'cancelled'`, jobID, u.ID).Scan(&raw, &status); err == sql.ErrNoRows {
		return bad(http.StatusNotFound, "NOT_FOUND", "计划不存在")
	} else if err != nil {
		return err
	}
	if status == "running" {
		return bad(http.StatusConflict, "JOB_RUNNING", "计划正在生成中，请稍后再试")
	}
	var payload aiProactivePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return errors.New("invalid ai proactive payload stored in database")
	}
	if payload.LastRunAt != "" {
		if last, err := time.Parse(time.RFC3339, payload.LastRunAt); err == nil && time.Since(last) < 30*time.Second {
			return bad(http.StatusTooManyRequests, "TOO_FREQUENT", "刚刚生成过，请 30 秒后再试")
		}
	}
	if _, err := s.DB.ExecContext(r.Context(), `UPDATE scheduled_jobs SET status='scheduled',run_at=UTC_TIMESTAMP(3),attempts=0 WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?) AND job_type='ai.proactive'`, jobID, u.ID); err != nil {
		return err
	}
	write(w, http.StatusAccepted, map[string]bool{"queued": true})
	return nil
}

func normalizeAIProactive(u User, x aiProactiveInput) (aiProactivePayload, time.Time, error) {
	kind := strings.TrimSpace(x.Kind)
	if kind == "" {
		kind = "daily_report"
	}
	if kind != "greeting" && kind != "daily_report" {
		return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "内容类型无效")
	}
	cadence := strings.TrimSpace(x.Cadence)
	if cadence == "" {
		cadence = "daily"
	}
	if !in(cadence, "daily", "weekdays", "weekly", "once") {
		return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "频率无效")
	}
	timezone := strings.TrimSpace(x.Timezone)
	if timezone == "" {
		timezone = u.Timezone
	}
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "时区无效")
	}
	prompt := strings.TrimSpace(x.Prompt)
	if len([]rune(prompt)) > 1000 {
		return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "补充要求不能超过 1000 字")
	}
	enabled := true
	if x.Enabled != nil {
		enabled = *x.Enabled
	}
	p := aiProactivePayload{
		Kind: kind, Cadence: cadence, Timezone: timezone, Prompt: prompt, Enabled: enabled,
	}
	if cadence == "once" {
		runAt, err := time.Parse(time.RFC3339, strings.TrimSpace(x.RunAt))
		if err != nil {
			return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "请选择一次性执行时间（含时区）")
		}
		runAt = runAt.UTC()
		now := time.Now().UTC()
		if runAt.Before(now.Add(time.Minute)) || runAt.After(now.AddDate(1, 0, 0)) {
			return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "执行时间需在 1 分钟后至 1 年内")
		}
		p.Time = runAt.In(loc).Format("15:04")
		p.Weekday = isoWeekday(runAt.In(loc))
		return p, runAt, nil
	}
	clock, err := time.Parse("15:04", strings.TrimSpace(x.Time))
	if err != nil {
		return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "时间格式应为 HH:MM")
	}
	p.Time = clock.Format("15:04")
	if cadence == "weekly" {
		if x.Weekday < 1 || x.Weekday > 7 {
			return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "请选择星期几")
		}
		p.Weekday = x.Weekday
	}
	next, err := nextAIProactiveRun(p, time.Now())
	if err != nil {
		return aiProactivePayload{}, time.Time{}, bad(http.StatusBadRequest, "VALIDATION_ERROR", "无法计算下次执行时间")
	}
	return p, next, nil
}

func isoWeekday(t time.Time) int {
	day := int(t.Weekday())
	if day == 0 {
		return 7
	}
	return day
}

func nextAIProactiveRun(p aiProactivePayload, after time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	clock, err := time.Parse("15:04", p.Time)
	if err != nil {
		return time.Time{}, err
	}
	local := after.In(loc)
	for offset := 0; offset <= 8; offset++ {
		day := local.AddDate(0, 0, offset)
		candidate := time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, loc)
		if !candidate.After(after) {
			continue
		}
		weekday := isoWeekday(candidate)
		switch p.Cadence {
		case "weekdays":
			if weekday > 5 {
				continue
			}
		case "weekly":
			if weekday != p.Weekday {
				continue
			}
		}
		return candidate.UTC(), nil
	}
	return time.Time{}, errors.New("no next run within 8 days")
}

func (s *Server) RunAIProactiveWorker(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	lastErrorLog := time.Time{}
	for {
		if err := s.processDueAIProactive(ctx); err != nil && ctx.Err() == nil {
			if time.Since(lastErrorLog) >= 30*time.Second {
				log.Printf("ai proactive worker tick failed; will retry")
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

func (s *Server) processDueAIProactive(ctx context.Context) error {
	// A process crash between claim and finish leaves a row in "running".
	// Return it to the queue after 15 minutes so a daily report is not lost.
	if _, err := s.DB.ExecContext(ctx, `UPDATE scheduled_jobs SET status='scheduled',run_at=UTC_TIMESTAMP(3)
		WHERE job_type='ai.proactive' AND status='running' AND run_at<DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 15 MINUTE)`); err != nil {
		return err
	}
	for i := 0; i < 5; i++ {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var jobID, ownerID, teamID string
		var raw []byte
		var attempts uint32
		err = tx.QueryRowContext(ctx, `SELECT HEX(id),HEX(owner_user_id),COALESCE(HEX(team_id),''),payload,attempts
			FROM scheduled_jobs WHERE job_type='ai.proactive' AND status='scheduled' AND run_at<=UTC_TIMESTAMP(3)
			ORDER BY run_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&jobID, &ownerID, &teamID, &raw, &attempts)
		if err == sql.ErrNoRows {
			_ = tx.Rollback()
			return nil
		}
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE scheduled_jobs SET status='running',attempts=attempts+1,run_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?)`, jobID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		s.executeAIProactiveJob(ctx, jobID, ownerID, teamID, raw, attempts+1)
	}
	return nil
}

func (s *Server) executeAIProactiveJob(ctx context.Context, jobID, ownerID, teamID string, raw []byte, attempts uint32) {
	var p aiProactivePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		s.finishAIProactiveFailure(ctx, jobID, p, "invalid_payload", attempts)
		return
	}
	var userStatus string
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM users WHERE id=UNHEX(?)`, ownerID).Scan(&userStatus); err == sql.ErrNoRows || userStatus != "active" {
		_, _ = s.DB.ExecContext(ctx, `UPDATE scheduled_jobs SET status='cancelled' WHERE id=UNHEX(?)`, jobID)
		return
	}
	var activeTeam int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_memberships m JOIN teams t ON t.id=m.team_id WHERE m.team_id=UNHEX(?) AND m.user_id=UNHEX(?) AND m.status='active' AND t.status='active'`, teamID, ownerID).Scan(&activeTeam); err != nil {
		s.finishAIProactiveFailure(ctx, jobID, p, "team_check_failed", attempts)
		return
	}
	if activeTeam != 1 {
		p.LastError = "team_unavailable"
		p.LastStatus = "paused"
		encoded, _ := json.Marshal(p)
		_, _ = s.DB.ExecContext(ctx, `UPDATE scheduled_jobs SET status='paused',payload=? WHERE id=UNHEX(?)`, encoded, jobID)
		return
	}
	genCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	title, body, mode, genErr := s.generateAIProactive(genCtx, ownerID, teamID, p)
	cancel()
	if genErr != nil {
		log.Printf("ai proactive generation fallback for job %s: %v", jobID, genErr)
	}
	if strings.TrimSpace(body) == "" {
		s.finishAIProactiveFailure(ctx, jobID, p, "empty_generation", attempts)
		return
	}
	p.LastRunAt = time.Now().UTC().Format(time.RFC3339)
	p.LastStatus = mode
	p.LastTitle = title
	p.LastBody = truncateRunes(body, 4000)
	p.LastError = ""
	if genErr != nil {
		p.LastError = truncateRunes(genErr.Error(), 300)
	}
	newStatus, runAt := "scheduled", time.Now().UTC()
	if p.Cadence == "once" {
		newStatus = "succeeded"
	} else {
		next, err := nextAIProactiveRun(p, time.Now())
		if err != nil {
			newStatus = "paused"
			p.LastError = "schedule_invalid"
		} else {
			runAt = next
		}
	}
	if !p.Enabled {
		newStatus = "paused"
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		s.finishAIProactiveFailure(ctx, jobID, p, "payload_encode_failed", attempts)
		return
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		s.finishAIProactiveFailure(ctx, jobID, p, "notification_failed", attempts)
		return
	}
	if err = notifyResource(tx, teamID, ownerID, "ai_proactive", title, body, "ai_proactive", jobID); err != nil {
		_ = tx.Rollback()
		s.finishAIProactiveFailure(ctx, jobID, p, "notification_failed", attempts)
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scheduled_jobs SET status=?,run_at=?,payload=?,attempts=0 WHERE id=UNHEX(?) AND status='running'`, newStatus, runAt, encoded, jobID); err != nil {
		_ = tx.Rollback()
		s.finishAIProactiveFailure(ctx, jobID, p, "schedule_update_failed", attempts)
		return
	}
	if err = tx.Commit(); err != nil {
		log.Printf("ai proactive commit failed for job %s", jobID)
	}
}

func (s *Server) finishAIProactiveFailure(ctx context.Context, jobID string, p aiProactivePayload, reason string, attempts uint32) {
	p.LastStatus = "failed"
	p.LastError = reason
	status := "failed"
	runAt := time.Now().UTC()
	if p.Enabled && p.Cadence != "once" && attempts < 3 {
		status = "scheduled"
		runAt = time.Now().UTC().Add(10 * time.Minute)
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE scheduled_jobs SET status=?,run_at=?,payload=?,attempts=? WHERE id=UNHEX(?) AND status='running'`, status, runAt, encoded, attempts, jobID)
}

func (s *Server) generateAIProactive(ctx context.Context, userID, teamID string, p aiProactivePayload) (string, string, string, error) {
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	var userName string
	_ = s.DB.QueryRowContext(ctx, `SELECT display_name FROM users WHERE id=UNHEX(?)`, userID).Scan(&userName)
	if userName == "" {
		userName = "你"
	}
	reportLabel := "AI 日报"
	if p.Cadence == "weekly" {
		reportLabel = "AI 周报"
	}
	title := reportLabel + " · " + now.Format("1月2日")
	if p.Kind == "greeting" {
		title = greetingWord(now.Hour()) + "，" + userName + " · " + now.Format("1月2日")
	}
	fallback := s.aiProactiveFallback(ctx, teamID, p, now)
	aiCtx := withAITeam(ctx, teamID)
	if !s.aiConfigured(aiCtx) {
		return title, fallback, "template", nil
	}
	teamCtx, ctxErr := s.aiTeamContext(aiCtx, teamID, userID, "")
	if ctxErr != nil {
		return title, fallback, "template", ctxErr
	}
	raw, err := json.Marshal(teamCtx)
	if err != nil {
		return title, fallback, "template", err
	}
	instruction := "写一份今日工作日报：今日重点（最多 3 条）、风险与阻塞、下一步建议；用短段落或短列表，不要 Markdown 表格，总字数不超过 500 字。"
	if p.Kind == "greeting" {
		instruction = "写一段 2–4 句的问候，语气自然、不油腻；可以点出 1–2 个今天值得关注的事项，但不要罗列全部数据。"
	}
	if p.Prompt != "" {
		instruction += "\n用户补充要求：" + p.Prompt
	}
	messages := []aiMessage{
		{Role: "system", Content: "你是 TeamMind 的 AI 工作助理。只能使用给定团队数据，不要编造；直接输出正文，不要加“以下是”。"},
		{Role: "user", Content: "当前时间：" + now.Format("2006-01-02 15:04 Monday") + "\n团队数据（JSON）：\n" + truncateRunes(string(raw), 8000) + "\n\n任务：" + instruction},
	}
	reply, _, aiErr := s.replyMessages(aiCtx, messages)
	reply = strings.TrimSpace(reply)
	if aiErr != nil || reply == "" {
		if aiErr == nil {
			aiErr = errors.New("empty AI reply")
		}
		return title, fallback, "template", aiErr
	}
	return title, truncateRunes(reply, 4000), "ai", nil
}

func greetingWord(hour int) string {
	switch {
	case hour < 5:
		return "夜深了"
	case hour < 12:
		return "早安"
	case hour < 18:
		return "下午好"
	default:
		return "晚上好"
	}
}

func (s *Server) aiProactiveFallback(ctx context.Context, teamID string, p aiProactivePayload, now time.Time) string {
	var teamName string
	_ = s.DB.QueryRowContext(ctx, `SELECT name FROM teams WHERE id=UNHEX(?)`, teamID).Scan(&teamName)
	var projects, tasks, blocked, pending int
	_ = s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM projects WHERE team_id=UNHEX(?) AND deleted_at IS NULL),
		(SELECT COUNT(*) FROM tasks WHERE team_id=UNHEX(?) AND deleted_at IS NULL),
		(SELECT COUNT(*) FROM tasks WHERE team_id=UNHEX(?) AND status='blocked' AND deleted_at IS NULL),
		((SELECT COUNT(*) FROM plan_change_proposals WHERE team_id=UNHEX(?) AND status='pending')+
		 (SELECT COUNT(*) FROM project_change_proposals WHERE team_id=UNHEX(?) AND status='pending')+
		 (SELECT COUNT(*) FROM member_change_proposals WHERE team_id=UNHEX(?) AND status='pending'))`,
		teamID, teamID, teamID, teamID, teamID, teamID).Scan(&projects, &tasks, &blocked, &pending)
	var b strings.Builder
	if p.Kind == "greeting" {
		b.WriteString(greetingWord(now.Hour()) + "。今天团队有 " + strconv.Itoa(tasks) + " 个任务在推进")
		if blocked > 0 {
			b.WriteString("，其中 " + strconv.Itoa(blocked) + " 个处于阻塞状态")
		}
		b.WriteString("。\n")
	} else {
		b.WriteString("【团队概况】\n")
		b.WriteString(teamName + "：" + strconv.Itoa(projects) + " 个项目、" + strconv.Itoa(tasks) + " 个任务，阻塞 " + strconv.Itoa(blocked) + " 个，待审批 " + strconv.Itoa(pending) + " 项。\n")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT t.title,COALESCE(u.display_name,'未分配'),DATE_FORMAT(t.due_date,'%m-%d')
		FROM tasks t LEFT JOIN users u ON u.id=t.assignee_id
		WHERE t.team_id=UNHEX(?) AND t.deleted_at IS NULL AND t.status='blocked'
		ORDER BY t.due_date IS NULL,t.due_date LIMIT 5`, teamID)
	if err == nil {
		items := make([]string, 0, 5)
		for rows.Next() {
			var title, assignee string
			var due sql.NullString
			if rows.Scan(&title, &assignee, &due) == nil {
				line := "- " + title + "（" + assignee + "）"
				if due.Valid {
					line += "，截止 " + due.String
				}
				items = append(items, line)
			}
		}
		rows.Close()
		if len(items) > 0 {
			b.WriteString("\n【风险 / 阻塞】\n" + strings.Join(items, "\n") + "\n")
		}
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT t.title,COALESCE(u.display_name,'未分配'),DATE_FORMAT(t.due_date,'%m-%d')
		FROM tasks t LEFT JOIN users u ON u.id=t.assignee_id
		WHERE t.team_id=UNHEX(?) AND t.deleted_at IS NULL AND t.status NOT IN ('done','cancelled')
		AND t.due_date IS NOT NULL AND t.due_date<=DATE_ADD(UTC_DATE(),INTERVAL 3 DAY)
		ORDER BY t.due_date LIMIT 5`, teamID)
	if err == nil {
		items := make([]string, 0, 5)
		for rows.Next() {
			var title, assignee, due string
			if rows.Scan(&title, &assignee, &due) == nil {
				items = append(items, "- "+title+"（"+assignee+"），截止 "+due)
			}
		}
		rows.Close()
		if len(items) > 0 {
			b.WriteString("\n【近 3 天到期】\n" + strings.Join(items, "\n") + "\n")
		}
	}
	if p.Kind == "greeting" {
		b.WriteString("\n需要我展开某条任务或帮你安排今天的工作，直接在 AI 助手里问我就行。")
	} else {
		b.WriteString("\n【建议】优先处理阻塞项与近 3 天到期的任务")
		if pending > 0 {
			b.WriteString("，并清理 " + strconv.Itoa(pending) + " 项待审批")
		}
		b.WriteString("。")
	}
	return b.String()
}
