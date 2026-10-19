package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type taskPlanSnapshot struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	Status      string  `json:"status"`
	Progress    int     `json:"progress"`
	AssigneeID  *string `json:"assigneeId"`
	StartDate   *string `json:"startDate"`
	DueDate     *string `json:"dueDate"`
	Milestone   bool    `json:"milestone"`
}

func snapshotForTask(t task) taskPlanSnapshot {
	return taskPlanSnapshot{Title: t.Title, Description: t.Description, Priority: t.Priority, Status: t.Status, Progress: t.Progress, AssigneeID: t.AssigneeID, StartDate: t.StartDate, DueDate: t.DueDate, Milestone: t.Milestone}
}

func applySnapshotPatch(base taskPlanSnapshot, title, description, priority, status, assigneeID, startDate, dueDate *string, progress *int, milestone *bool) taskPlanSnapshot {
	if title != nil {
		base.Title = *title
	}
	if description != nil {
		base.Description = *description
	}
	if priority != nil {
		base.Priority = *priority
	}
	if status != nil {
		base.Status = *status
	}
	if progress != nil {
		base.Progress = *progress
	}
	if assigneeID != nil {
		base.AssigneeID = nil
		if strings.TrimSpace(*assigneeID) != "" {
			v := strings.ToUpper(strings.TrimSpace(*assigneeID))
			base.AssigneeID = &v
		}
	}
	if startDate != nil {
		base.StartDate = datePointer(*startDate)
	}
	if dueDate != nil {
		base.DueDate = datePointer(*dueDate)
	}
	if milestone != nil {
		base.Milestone = *milestone
	}
	return base
}

func datePointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func (s *Server) validateSnapshot(ctx context.Context, tx *sql.Tx, tid string, snap taskPlanSnapshot) error {
	snap.Title = strings.TrimSpace(snap.Title)
	if snap.Title == "" || len([]rune(snap.Title)) > 240 || len(snap.Description) > 20000 || !in(snap.Priority, "low", "medium", "high", "urgent") || !in(snap.Status, "todo", "doing", "blocked", "review", "done", "cancelled") || snap.Progress < 0 || snap.Progress > 100 {
		return bad(400, "VALIDATION_ERROR", "标题、描述、优先级、任务状态或进度无效")
	}
	if snap.StartDate != nil && !validDate(*snap.StartDate) || snap.DueDate != nil && !validDate(*snap.DueDate) {
		return bad(400, "VALIDATION_ERROR", "日期无效")
	}
	if snap.StartDate != nil && snap.DueDate != nil && *snap.DueDate < *snap.StartDate {
		return bad(400, "VALIDATION_ERROR", "截止日期不能早于开始日期")
	}
	if snap.AssigneeID != nil {
		if !validID(*snap.AssigneeID) {
			return bad(400, "INVALID_ID", "负责人 ID 无效")
		}
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) FOR UPDATE`, tid, *snap.AssigneeID).Scan(&status)
		if err == sql.ErrNoRows {
			return bad(400, "ASSIGNEE_NOT_ACTIVE", "负责人必须是当前团队的有效成员")
		}
		if err != nil {
			return err
		}
		if status != "active" {
			return bad(400, "ASSIGNEE_NOT_ACTIVE", "负责人必须是当前团队的有效成员")
		}
	}
	return nil
}

func marshalSnapshot(snap taskPlanSnapshot) []byte {
	data, _ := json.Marshal(snap)
	return data
}

func decodePlanSnapshot(data []byte, fallback taskPlanSnapshot) (taskPlanSnapshot, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return taskPlanSnapshot{}, err
	}
	if raw, ok := fields["title"]; ok {
		if err := json.Unmarshal(raw, &fallback.Title); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	if raw, ok := fields["description"]; ok {
		if err := json.Unmarshal(raw, &fallback.Description); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	if raw, ok := fields["priority"]; ok {
		if err := json.Unmarshal(raw, &fallback.Priority); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	if raw, ok := fields["status"]; ok {
		if err := json.Unmarshal(raw, &fallback.Status); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	if raw, ok := fields["progress"]; ok {
		if err := json.Unmarshal(raw, &fallback.Progress); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	if raw, ok := fields["assigneeId"]; ok {
		if err := json.Unmarshal(raw, &fallback.AssigneeID); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	if raw, ok := fields["startDate"]; ok {
		if err := json.Unmarshal(raw, &fallback.StartDate); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	if raw, ok := fields["dueDate"]; ok {
		if err := json.Unmarshal(raw, &fallback.DueDate); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	if raw, ok := fields["milestone"]; ok {
		if err := json.Unmarshal(raw, &fallback.Milestone); err != nil {
			return taskPlanSnapshot{}, err
		}
	}
	return fallback, nil
}

func samePlan(a, b taskPlanSnapshot) bool {
	return ptrString(a.AssigneeID) == ptrString(b.AssigneeID) && ptrString(a.StartDate) == ptrString(b.StartDate) && ptrString(a.DueDate) == ptrString(b.DueDate) && a.Title == b.Title && a.Description == b.Description && a.Priority == b.Priority && a.Status == b.Status && a.Progress == b.Progress && a.Milestone == b.Milestone
}

func ptrString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func taskForUpdate(ctx context.Context, tx *sql.Tx, tid, taskID string) (task, error) {
	rows, err := tx.QueryContext(ctx, taskSelect+` WHERE t.id=UNHEX(?) AND t.team_id=UNHEX(?) AND t.deleted_at IS NULL FOR UPDATE`, taskID, tid)
	if err != nil {
		return task{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return task{}, err
		}
		return task{}, sql.ErrNoRows
	}
	return scanTask(rows)
}

func (s *Server) proposals(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {
	if len(p) == 3 && r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(p.id),HEX(p.task_id),t.title,HEX(p.proposed_by),p.reason,p.impact_summary,p.proposed_snapshot,p.status,p.task_version,p.created_at,p.resolved_at,t.status,t.progress FROM plan_change_proposals p JOIN tasks t ON t.id=p.task_id WHERE p.team_id=UNHEX(?) ORDER BY p.created_at DESC LIMIT 100`, tid)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var id, taskID, title, author, reason, impact, status string
			var snap json.RawMessage
			var version int
			var created time.Time
			var resolved sql.NullTime
			var taskStatus string
			var taskProgress int
			if e = rows.Scan(&id, &taskID, &title, &author, &reason, &impact, &snap, &status, &version, &created, &resolved, &taskStatus, &taskProgress); e != nil {
				return e
			}
			proposed, decodeErr := decodePlanSnapshot(snap, taskPlanSnapshot{Title: title, Status: taskStatus, Progress: taskProgress})
			if decodeErr != nil {
				return decodeErr
			}
			out = append(out, map[string]any{"id": id, "taskId": taskID, "taskTitle": title, "proposedBy": author, "reason": reason, "impactSummary": impact, "proposed": proposed, "status": status, "taskVersion": version, "createdAt": created, "resolvedAt": resolved.Time})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if len(p) == 3 && r.Method == "POST" {
		var x struct {
			TaskID        string  `json:"taskId"`
			Title         *string `json:"title"`
			Description   *string `json:"description"`
			Priority      *string `json:"priority"`
			Status        *string `json:"status"`
			AssigneeID    *string `json:"assigneeId"`
			StartDate     *string `json:"startDate"`
			DueDate       *string `json:"dueDate"`
			Progress      *int    `json:"progress"`
			Milestone     *bool   `json:"milestone"`
			Reason        string  `json:"reason"`
			ImpactSummary string  `json:"impactSummary"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		x.Reason = strings.TrimSpace(x.Reason)
		if !validID(x.TaskID) || len([]rune(x.Reason)) < 5 || len([]rune(x.Reason)) > 5000 || len([]rune(x.ImpactSummary)) > 5000 {
			return bad(400, "VALIDATION_ERROR", "请填写有效任务和变更原因")
		}
		t, e := s.taskByID(r, tid, x.TaskID)
		if e != nil {
			return e
		}
		if role == "member" && t.CreatedBy != u.ID && (t.AssigneeID == nil || *t.AssigneeID != u.ID) {
			return bad(403, "TASK_ACCESS_DENIED", "只能为自己创建或负责的任务提交提案")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		currentRole, e := activeTeamRoleTx(r.Context(), tx, tid, u)
		if e != nil {
			return e
		}
		current, e := taskForUpdate(r.Context(), tx, tid, x.TaskID)
		if e != nil {
			if e == sql.ErrNoRows {
				return bad(404, "NOT_FOUND", "任务不存在")
			}
			return e
		}
		if currentRole == "member" && current.CreatedBy != u.ID && (current.AssigneeID == nil || *current.AssigneeID != u.ID) {
			return bad(403, "TASK_ACCESS_DENIED", "只能为自己创建或负责的任务提交提案")
		}
		if current.Version != t.Version {
			return bad(409, "VERSION_CONFLICT", "任务已变化，请重新加载后提交")
		}
		beforePlan := snapshotForTask(current)
		proposed := applySnapshotPatch(beforePlan, x.Title, x.Description, x.Priority, x.Status, x.AssigneeID, x.StartDate, x.DueDate, x.Progress, x.Milestone)
		proposed.Title = strings.TrimSpace(proposed.Title)
		if e = s.validateSnapshot(r.Context(), tx, tid, proposed); e != nil {
			return e
		}
		if samePlan(beforePlan, proposed) {
			return bad(400, "NO_PLAN_CHANGES", "至少修改一个任务字段")
		}
		before, _ := json.Marshal(map[string]any{"plan": beforePlan, "version": current.Version})
		after := marshalSnapshot(proposed)
		pid := id()
		_, e = tx.ExecContext(r.Context(), `INSERT INTO plan_change_proposals(id,team_id,project_id,task_id,task_version,proposed_by,reason,impact_summary,before_snapshot,proposed_snapshot) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,UNHEX(?),?,?,?,?)`, pid, tid, t.ProjectID, x.TaskID, t.Version, u.ID, x.Reason, x.ImpactSummary, before, after)
		if e != nil {
			if strings.Contains(e.Error(), "Duplicate") {
				return bad(409, "PROPOSAL_PENDING", "该任务已有待处理提案")
			}
			return e
		}
		if e = addAudit(tx, u.ID, tid, "proposal.create", "proposal", pid); e != nil {
			return e
		}
		if e = notifyAdmins(tx, tid, u.ID, "新的计划变更待审批", t.Title, pid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 201, map[string]string{"id": pid})
		return nil
	}
	if len(p) == 5 && p[4] == "resubmit" && r.Method == "POST" {
		pid := p[3]
		if !validID(pid) {
			return bad(404, "NOT_FOUND", "提案不存在")
		}
		var x struct {
			Title         *string `json:"title"`
			Description   *string `json:"description"`
			Priority      *string `json:"priority"`
			Status        *string `json:"status"`
			AssigneeID    *string `json:"assigneeId"`
			StartDate     *string `json:"startDate"`
			DueDate       *string `json:"dueDate"`
			Progress      *int    `json:"progress"`
			Milestone     *bool   `json:"milestone"`
			Reason        string  `json:"reason"`
			ImpactSummary string  `json:"impactSummary"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		x.Reason = strings.TrimSpace(x.Reason)
		if len([]rune(x.Reason)) < 5 || len([]rune(x.Reason)) > 5000 || len([]rune(x.ImpactSummary)) > 5000 {
			return bad(400, "VALIDATION_ERROR", "请填写原因和影响说明")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = activeTeamRoleTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		var taskID, author, status string
		var version int
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(task_id),HEX(proposed_by),status,task_version FROM plan_change_proposals WHERE id=UNHEX(?) AND team_id=UNHEX(?) FOR UPDATE`, pid, tid).Scan(&taskID, &author, &status, &version)
		if e == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "提案不存在")
		}
		if e != nil {
			return e
		}
		if author != u.ID {
			return bad(403, "PROPOSAL_OWNER_REQUIRED", "只有提交人可以补充提案")
		}
		if status != "needs_info" {
			return bad(409, "PROPOSAL_STATE_CONFLICT", "提案当前不需要补充信息")
		}
		current, taskErr := taskForUpdate(r.Context(), tx, tid, taskID)
		if taskErr != nil {
			if taskErr == sql.ErrNoRows {
				return bad(409, "VERSION_CONFLICT", "任务已变化，请重新提交提案")
			}
			return taskErr
		}
		if current.Version != version {
			return bad(409, "VERSION_CONFLICT", "任务已变化，请重新提交提案")
		}
		base := snapshotForTask(current)
		var previous []byte
		if e = tx.QueryRowContext(r.Context(), `SELECT proposed_snapshot FROM plan_change_proposals WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, pid, tid).Scan(&previous); e != nil {
			return e
		}
		base, e = decodePlanSnapshot(previous, base)
		if e != nil {
			return e
		}
		proposed := applySnapshotPatch(base, x.Title, x.Description, x.Priority, x.Status, x.AssigneeID, x.StartDate, x.DueDate, x.Progress, x.Milestone)
		proposed.Title = strings.TrimSpace(proposed.Title)
		if e = s.validateSnapshot(r.Context(), tx, tid, proposed); e != nil {
			return e
		}
		if samePlan(snapshotForTask(current), proposed) {
			return bad(400, "NO_PLAN_CHANGES", "至少修改一个任务字段")
		}
		snapshot := marshalSnapshot(proposed)
		_, e = tx.ExecContext(r.Context(), `UPDATE plan_change_proposals SET status='pending',resolved_at=NULL,reason=?,impact_summary=?,proposed_snapshot=? WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, x.Reason, x.ImpactSummary, snapshot, pid, tid)
		if e != nil {
			if strings.Contains(e.Error(), "Duplicate") {
				return bad(409, "PROPOSAL_PENDING", "该任务已有待处理提案")
			}
			return e
		}
		if e = addAudit(tx, u.ID, tid, "proposal.resubmit", "proposal", pid); e != nil {
			return e
		}
		if e = notifyAdmins(tx, tid, u.ID, "计划变更已补充", x.Reason, pid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]string{"status": "pending"})
		return nil
	}
	if len(p) == 5 && p[4] == "decision" && r.Method == "POST" {
		if e := admin(role); e != nil {
			return e
		}
		pid := p[3]
		if !validID(pid) {
			return bad(404, "NOT_FOUND", "提案不存在")
		}
		var x struct {
			Decision    string  `json:"decision"`
			Reason      string  `json:"reason"`
			Title       *string `json:"title"`
			Description *string `json:"description"`
			Priority    *string `json:"priority"`
			Status      *string `json:"status"`
			AssigneeID  *string `json:"assigneeId"`
			StartDate   *string `json:"startDate"`
			DueDate     *string `json:"dueDate"`
			Progress    *int    `json:"progress"`
			Milestone   *bool   `json:"milestone"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if !in(x.Decision, "approved", "edited_approved", "rejected", "needs_info") || len([]rune(x.Reason)) > 5000 {
			return bad(400, "VALIDATION_ERROR", "审批参数无效")
		}
		if (x.Decision == "rejected" || x.Decision == "needs_info") && len([]rune(strings.TrimSpace(x.Reason))) < 5 {
			return bad(400, "VALIDATION_ERROR", "拒绝或要求补充时请填写至少 5 字的说明")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		currentRole, roleErr := activeTeamRoleTx(r.Context(), tx, tid, u)
		if roleErr != nil {
			return roleErr
		}
		if e = admin(currentRole); e != nil {
			return e
		}
		var taskID, author, status string
		var version int
		var proposed []byte
		e = tx.QueryRowContext(r.Context(), `SELECT HEX(task_id),HEX(proposed_by),status,task_version,proposed_snapshot FROM plan_change_proposals WHERE id=UNHEX(?) AND team_id=UNHEX(?) FOR UPDATE`, pid, tid).Scan(&taskID, &author, &status, &version, &proposed)
		if e == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "提案不存在")
		}
		if e != nil {
			return e
		}
		if status != "pending" {
			return bad(409, "PROPOSAL_RESOLVED", "提案已处理")
		}
		current, e := taskForUpdate(r.Context(), tx, tid, taskID)
		if e != nil {
			if e == sql.ErrNoRows {
				return bad(409, "TASK_CHANGED", "任务已不存在")
			}
			return e
		}
		if current.Version != version {
			return bad(409, "VERSION_CONFLICT", "任务计划已变化，请重新提交提案")
		}
		snap, e := decodePlanSnapshot(proposed, snapshotForTask(current))
		if e != nil {
			return e
		}
		if x.Decision == "edited_approved" {
			snap = applySnapshotPatch(snap, x.Title, x.Description, x.Priority, x.Status, x.AssigneeID, x.StartDate, x.DueDate, x.Progress, x.Milestone)
			snap.Title = strings.TrimSpace(snap.Title)
		}
		if x.Decision == "approved" || x.Decision == "edited_approved" {
			if e = s.validateSnapshot(r.Context(), tx, tid, snap); e != nil {
				return e
			}
		}
		before, _ := json.Marshal(map[string]any{"plan": snapshotForTask(current), "version": version})
		after := marshalSnapshot(snap)
		newStatus := x.Decision
		if x.Decision == "edited_approved" {
			newStatus = "approved"
		}
		if x.Decision == "approved" || x.Decision == "edited_approved" {
			assigneeID := any(nil)
			if snap.AssigneeID != nil {
				assigneeID = *snap.AssigneeID
			}
			res, updateErr := tx.ExecContext(r.Context(), `UPDATE tasks SET title=?,description=?,priority=?,status=?,progress=?,assignee_id=UNHEX(?),start_date=?,due_date=?,is_milestone=?,version=version+1 WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND version=?`, snap.Title, snap.Description, snap.Priority, snap.Status, snap.Progress, assigneeID, nullableDate(ptrString(snap.StartDate)), nullableDate(ptrString(snap.DueDate)), snap.Milestone, taskID, tid, version)
			e = updateErr
			if e != nil {
				return e
			}
			updated, _ := res.RowsAffected()
			if updated != 1 {
				return bad(409, "VERSION_CONFLICT", "任务计划已被其他人修改")
			}
			_, e = tx.ExecContext(r.Context(), `INSERT INTO task_plan_versions(team_id,task_id,version,snapshot,change_source,source_ref,actor_id) VALUES(UNHEX(?),UNHEX(?),?,?, 'proposal_approved',UNHEX(?),UNHEX(?))`, tid, taskID, version+1, after, pid, u.ID)
			if e != nil {
				return e
			}
		}
		_, e = tx.ExecContext(r.Context(), `UPDATE plan_change_proposals SET status=?,resolved_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?)`, newStatus, pid)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO plan_change_approvals(id,team_id,proposal_id,approver_id,decision,reason,before_snapshot,after_snapshot) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,?,?,?)`, id(), tid, pid, u.ID, x.Decision, x.Reason, before, after)
		if e != nil {
			return e
		}
		if e = addAudit(tx, u.ID, tid, "proposal."+x.Decision, "proposal", pid); e != nil {
			return e
		}
		recipients := map[string]bool{author: true}
		if current.AssigneeID != nil {
			recipients[*current.AssigneeID] = true
		}
		if snap.AssigneeID != nil {
			recipients[*snap.AssigneeID] = true
		}
		for recipient := range recipients {
			if recipient == u.ID {
				continue
			}
			e = notifyRecipient(tx, tid, recipient, "plan", "计划变更审批结果", x.Decision, pid)
			if e != nil {
				return e
			}
		}
		payload, _ := json.Marshal(map[string]string{"proposalId": pid, "decision": x.Decision})
		_, e = tx.ExecContext(r.Context(), `INSERT INTO outbox_events(id,team_id,event_type,aggregate_type,aggregate_id,payload) VALUES(UNHEX(?),UNHEX(?),'plan.decided','proposal',UNHEX(?),?)`, id(), tid, pid, payload)
		if e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]string{"status": newStatus})
		return nil
	}
	return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}
func notifyAdmins(tx *sql.Tx, tid, actor, title, body, pid string) error {
	rows, e := tx.Query(`SELECT HEX(user_id) FROM team_memberships WHERE team_id=UNHEX(?) AND role IN ('team_owner','team_admin') AND status='active'`, tid)
	if e != nil {
		return e
	}
	var recipients []string
	for rows.Next() {
		var uid string
		if e = rows.Scan(&uid); e != nil {
			rows.Close()
			return e
		}
		if uid != actor {
			recipients = append(recipients, uid)
		}
	}
	rows.Close()
	for _, uid := range recipients {
		e = notifyRecipient(tx, tid, uid, "approval", title, body, pid)
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) comments(w http.ResponseWriter, r *http.Request, u User, tid, taskID string) error {
	if _, e := s.taskByID(r, tid, taskID); e != nil {
		return e
	}
	if r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(c.id),HEX(c.author_id),u.display_name,c.body,c.created_at FROM task_comments c JOIN users u ON u.id=c.author_id WHERE c.team_id=UNHEX(?) AND c.task_id=UNHEX(?) AND c.deleted_at IS NULL ORDER BY c.created_at`, tid, taskID)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		for rows.Next() {
			var cid, author, name, text string
			var at time.Time
			if e = rows.Scan(&cid, &author, &name, &text, &at); e != nil {
				return e
			}
			out = append(out, map[string]any{"id": cid, "authorId": author, "authorName": name, "body": text, "createdAt": at})
		}
		write(w, 200, out)
		return rows.Err()
	}
	if r.Method == "POST" {
		var x struct {
			Body string `json:"body"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		x.Body = strings.TrimSpace(x.Body)
		if x.Body == "" || len([]rune(x.Body)) > 10000 {
			return bad(400, "VALIDATION_ERROR", "评论需为 1–10000 字")
		}
		cid := id()
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = activeTeamRoleTx(r.Context(), tx, tid, u); e != nil {
			return e
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO task_comments(id,team_id,task_id,author_id,body) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?)`, cid, tid, taskID, u.ID, x.Body)
		if e != nil {
			return e
		}
		if e = addAudit(tx, u.ID, tid, "task.comment.create", "task_comment", cid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 201, map[string]string{"id": cid})
		return nil
	}
	return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}

func (s *Server) commentByID(w http.ResponseWriter, r *http.Request, u User, tid, taskID, commentID string) error {
	if !validID(commentID) {
		return bad(404, "NOT_FOUND", "评论不存在")
	}
	if r.Method != "PATCH" && r.Method != "DELETE" {
		return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	role, err := activeTeamRoleTx(r.Context(), tx, tid, u)
	if err != nil {
		return err
	}
	var author string
	err = tx.QueryRowContext(r.Context(), `SELECT HEX(author_id) FROM task_comments WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND task_id=UNHEX(?) AND deleted_at IS NULL FOR UPDATE`, commentID, tid, taskID).Scan(&author)
	if err == sql.ErrNoRows {
		return bad(404, "NOT_FOUND", "评论不存在")
	}
	if err != nil {
		return err
	}
	if role != "team_owner" && role != "team_admin" && author != u.ID {
		return bad(403, "COMMENT_OWNER_REQUIRED", "只能修改或删除自己的评论")
	}
	action := "task.comment.delete"
	if r.Method == "PATCH" {
		var input struct {
			Body string `json:"body"`
		}
		if err = body(w, r, &input); err != nil {
			return err
		}
		input.Body = strings.TrimSpace(input.Body)
		if input.Body == "" || len([]rune(input.Body)) > 10000 {
			return bad(400, "VALIDATION_ERROR", "评论需为 1–10000 字")
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE task_comments SET body=? WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND task_id=UNHEX(?) AND deleted_at IS NULL`, input.Body, commentID, tid, taskID); err != nil {
			return err
		}
		action = "task.comment.update"
	} else {
		if _, err = tx.ExecContext(r.Context(), `UPDATE task_comments SET deleted_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND task_id=UNHEX(?) AND deleted_at IS NULL`, commentID, tid, taskID); err != nil {
			return err
		}
	}
	if err = addAudit(tx, u.ID, tid, action, "task_comment", commentID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, 200, map[string]bool{map[string]string{"PATCH": "saved", "DELETE": "deleted"}[r.Method]: true})
	return nil
}
func (s *Server) dependencies(w http.ResponseWriter, r *http.Request, u User, tid, taskID string) error {
	_, e := s.taskByID(r, tid, taskID)
	if e != nil {
		return e
	}
	if r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(depends_on_task_id) FROM task_dependencies WHERE team_id=UNHEX(?) AND task_id=UNHEX(?)`, tid, taskID)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var v string
			if e = rows.Scan(&v); e != nil {
				return e
			}
			out = append(out, v)
		}
		write(w, 200, out)
		return rows.Err()
	}
	if r.Method == "POST" {
		var x struct {
			DependsOnTaskID string `json:"dependsOnTaskId"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if !validID(x.DependsOnTaskID) || x.DependsOnTaskID == taskID {
			return bad(400, "VALIDATION_ERROR", "依赖任务无效")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		// Every graph writer takes the team row lock, so two concurrent inserts
		// cannot each pass the cycle check against a stale graph.
		var lockID string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM teams WHERE id=UNHEX(?) FOR UPDATE`, tid).Scan(&lockID); e != nil {
			return e
		}
		current, e := taskForUpdate(r.Context(), tx, tid, taskID)
		if e == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "任务不存在")
		}
		if e != nil {
			return e
		}
		canContribute, e := canContributeTaskTx(r.Context(), tx, tid, u, current)
		if e != nil {
			return e
		}
		if !canContribute {
			return bad(403, "TASK_ACCESS_DENIED", "只有任务创建者、负责人、协作者或团队管理员可以修改依赖")
		}
		if _, e = taskForUpdate(r.Context(), tx, tid, x.DependsOnTaskID); e == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "依赖任务不存在")
		}
		if e != nil {
			return e
		}
		var cycle int
		e = tx.QueryRowContext(r.Context(), `WITH RECURSIVE ancestors(node) AS (SELECT depends_on_task_id FROM task_dependencies WHERE team_id=UNHEX(?) AND task_id=UNHEX(?) UNION DISTINCT SELECT d.depends_on_task_id FROM task_dependencies d JOIN ancestors a ON d.task_id=a.node WHERE d.team_id=UNHEX(?)) SELECT COUNT(*) FROM ancestors WHERE node=UNHEX(?)`, tid, x.DependsOnTaskID, tid, taskID).Scan(&cycle)
		if e != nil {
			return e
		}
		if cycle > 0 {
			return bad(409, "DEPENDENCY_CYCLE", "此依赖会形成循环")
		}
		_, e = tx.ExecContext(r.Context(), `INSERT INTO task_dependencies(team_id,task_id,depends_on_task_id) VALUES(UNHEX(?),UNHEX(?),UNHEX(?))`, tid, taskID, x.DependsOnTaskID)
		if e != nil {
			return bad(409, "DEPENDENCY_EXISTS", "依赖已存在或形成冲突")
		}
		if e = addAudit(tx, u.ID, tid, "task.dependency.add", "task", taskID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 201, map[string]bool{"added": true})
		return nil
	}
	if r.Method == "DELETE" {
		var x struct {
			DependsOnTaskID string `json:"dependsOnTaskId"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if !validID(x.DependsOnTaskID) || x.DependsOnTaskID == taskID {
			return bad(400, "VALIDATION_ERROR", "依赖任务无效")
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var lockID string
		if e = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM teams WHERE id=UNHEX(?) FOR UPDATE`, tid).Scan(&lockID); e != nil {
			return e
		}
		current, e := taskForUpdate(r.Context(), tx, tid, taskID)
		if e == sql.ErrNoRows {
			return bad(404, "NOT_FOUND", "任务不存在")
		}
		if e != nil {
			return e
		}
		canContribute, e := canContributeTaskTx(r.Context(), tx, tid, u, current)
		if e != nil {
			return e
		}
		if !canContribute {
			return bad(403, "TASK_ACCESS_DENIED", "只有任务创建者、负责人、协作者或团队管理员可以修改依赖")
		}
		res, e := tx.ExecContext(r.Context(), `DELETE FROM task_dependencies WHERE team_id=UNHEX(?) AND task_id=UNHEX(?) AND depends_on_task_id=UNHEX(?)`, tid, taskID, x.DependsOnTaskID)
		if e != nil {
			return e
		}
		count, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if count != 1 {
			return bad(404, "NOT_FOUND", "依赖不存在")
		}
		if e = addAudit(tx, u.ID, tid, "task.dependency.remove", "task", taskID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"removed": true})
		return nil
	}
	return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}
func (s *Server) history(w http.ResponseWriter, r *http.Request, tid, taskID string) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	rows, e := s.DB.QueryContext(r.Context(), `SELECT version,snapshot,change_source,created_at FROM task_plan_versions WHERE team_id=UNHEX(?) AND task_id=UNHEX(?) ORDER BY version`, tid, taskID)
	if e != nil {
		return e
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var version int
		var snap json.RawMessage
		var source string
		var at time.Time
		if e = rows.Scan(&version, &snap, &source, &at); e != nil {
			return e
		}
		out = append(out, map[string]any{"version": version, "snapshot": snap, "source": source, "createdAt": at})
	}
	write(w, 200, out)
	return rows.Err()
}
