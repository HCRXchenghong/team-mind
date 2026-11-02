package live

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ganttInsights sends the currently filtered official schedule to the model
// and returns a reviewer-style analysis. It is read-only: no task, project or
// proposal row is written, and the model never receives member emails.
func (s *Server) ganttInsights(w http.ResponseWriter, r *http.Request, u User, teamID, role string) error {
	if e := method(r, "POST"); e != nil {
		return e
	}
	if role == "" {
		return bad(http.StatusForbidden, "TEAM_ACCESS_DENIED", "无权访问该公司")
	}
	var input struct {
		ProjectID  string `json:"projectId"`
		Status     string `json:"status"`
		AssigneeID string `json:"assigneeId"`
		DateFrom   string `json:"dateFrom"`
		DateTo     string `json:"dateTo"`
	}
	if e := body(w, r, &input); e != nil {
		return e
	}
	if input.ProjectID != "" && !validID(input.ProjectID) {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "项目筛选无效")
	}
	if input.AssigneeID != "" && input.AssigneeID != "unassigned" && !validID(input.AssigneeID) {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "负责人筛选无效")
	}
	aiCtx := withAITeam(r.Context(), teamID)
	if !s.aiConfigured(aiCtx) {
		return bad(http.StatusServiceUnavailable, "AI_NOT_CONFIGURED", "AI 服务尚未配置，请联系平台超管在 AI 管理中启用供应商")
	}
	query := `SELECT t.title,t.status,t.priority,t.progress,
		DATE_FORMAT(t.start_date,'%Y-%m-%d'),DATE_FORMAT(t.due_date,'%Y-%m-%d'),
		t.is_milestone,COALESCE(u.display_name,''),COALESCE(p.name,'')
		FROM tasks t
		LEFT JOIN users u ON u.id=t.assignee_id
		LEFT JOIN projects p ON p.id=t.project_id
		WHERE t.team_id=UNHEX(?) AND t.deleted_at IS NULL`
	args := []any{teamID}
	if input.ProjectID != "" {
		query += " AND t.project_id=UNHEX(?)"
		args = append(args, input.ProjectID)
	}
	if input.Status != "" {
		query += " AND t.status=?"
		args = append(args, input.Status)
	}
	if input.AssigneeID == "unassigned" {
		query += " AND t.assignee_id IS NULL"
	} else if input.AssigneeID != "" {
		query += " AND t.assignee_id=UNHEX(?)"
		args = append(args, input.AssigneeID)
	}
	if input.DateFrom != "" {
		query += " AND (t.due_date IS NULL OR t.due_date>=?)"
		args = append(args, input.DateFrom)
	}
	if input.DateTo != "" {
		query += " AND (t.start_date IS NULL OR t.start_date<=?)"
		args = append(args, input.DateTo)
	}
	query += " ORDER BY t.start_date IS NULL,t.start_date,t.due_date IS NULL,t.due_date LIMIT 200"
	rows, e := s.DB.QueryContext(r.Context(), query, args...)
	if e != nil {
		return e
	}
	defer rows.Close()
	type scheduleRow struct {
		Title, Status, Priority string
		Progress                int
		Start, Due              sql.NullString
		Milestone               bool
		Assignee, Project       string
	}
	items := make([]scheduleRow, 0)
	for rows.Next() {
		var item scheduleRow
		if e = rows.Scan(&item.Title, &item.Status, &item.Priority, &item.Progress, &item.Start, &item.Due, &item.Milestone, &item.Assignee, &item.Project); e != nil {
			return e
		}
		items = append(items, item)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if len(items) == 0 {
		return bad(http.StatusBadRequest, "GANTT_EMPTY", "当前筛选没有任务，请先调整筛选条件")
	}
	var data strings.Builder
	fmt.Fprintf(&data, "今天：%s\n任务数：%d\n", time.Now().Format("2006-01-02"), len(items))
	for index, item := range items {
		if data.Len() > 14000 {
			break
		}
		assignee := item.Assignee
		if assignee == "" {
			assignee = "未分配"
		}
		start := item.Start.String
		if start == "" {
			start = "无"
		}
		due := item.Due.String
		if due == "" {
			due = "无"
		}
		milestone := ""
		if item.Milestone {
			milestone = "，里程碑"
		}
		fmt.Fprintf(&data, "%d. %s｜项目：%s｜负责人：%s｜状态：%s｜优先级：%s｜进度：%d%%｜开始：%s｜截止：%s%s\n",
			index+1, item.Title, defaultText(item.Project, "未归属项目"), assignee, item.Status, item.Priority, item.Progress, start, due, milestone)
	}
	messages := []aiMessage{
		{Role: "system", Content: "你是资深项目排期与交付顾问。你只能使用用户提供的任务清单，不得编造任务、成员、依赖或日期。当前没有任何工具，也不要声称自己修改了任何数据。请用中文输出：\\n1) 一句话总体判断；\\n2) 风险清单，每条写清任务名、原因和影响（没有风险就写“未发现明显风险”）；\\n3) 3–6 条可直接执行的调整建议，必要时给出具体日期或负责人；\\n4) 需要人工确认的信息。\\n控制在 500 字以内，避免空话。"},
		{Role: "user", Content: "请检查这份甘特图排期：\\n" + data.String()},
	}
	raw, model, e := s.aiCompletionJSON(aiCtx, messages, nil, 1000)
	if e != nil {
		return e
	}
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &completion) != nil || len(completion.Choices) != 1 {
		return bad(http.StatusBadGateway, "AI_FAILED", "模型返回格式无效")
	}
	analysis := strings.TrimSpace(completion.Choices[0].Message.Content)
	if analysis == "" {
		return bad(http.StatusBadGateway, "AI_EMPTY_RESPONSE", "模型没有返回排期分析，请重试")
	}
	write(w, http.StatusOK, map[string]any{
		"analysis":    truncateRunes(analysis, 8000),
		"taskCount":   len(items),
		"model":       model,
		"generatedAt": time.Now().UTC(),
	})
	return nil
}

func defaultText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
