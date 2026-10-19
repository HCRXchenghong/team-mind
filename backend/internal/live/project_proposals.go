package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type projectSnapshot struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	StartDate   *string `json:"startDate"`
	DueDate     *string `json:"dueDate"`
}

func projectSnapshotFrom(p project) projectSnapshot {
	return projectSnapshot{Name: p.Name, Description: p.Description, Status: p.Status, StartDate: p.StartDate, DueDate: p.DueDate}
}

func applyProjectSnapshot(base projectSnapshot, input projectProposalInput) projectSnapshot {
	if input.Name != nil {
		base.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		base.Description = *input.Description
	}
	if input.Status != nil {
		base.Status = *input.Status
	}
	if input.StartDate != nil {
		base.StartDate = datePointer(*input.StartDate)
	}
	if input.DueDate != nil {
		base.DueDate = datePointer(*input.DueDate)
	}
	return base
}

type projectProposalInput struct {
	ProjectID     string  `json:"projectId"`
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Status        *string `json:"status,omitempty"`
	StartDate     *string `json:"startDate,omitempty"`
	DueDate       *string `json:"dueDate,omitempty"`
	Reason        string  `json:"reason"`
	ImpactSummary string  `json:"impactSummary"`
	SourceRef     string  `json:"-"`
	AgentProvider string  `json:"-"`
	AgentModel    string  `json:"-"`
}

func validateProjectSnapshot(s projectSnapshot) error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || len([]rune(s.Name)) > 200 || len([]rune(s.Description)) > 10000 || !in(s.Status, "planning", "active", "paused", "completed", "archived") {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "项目名称、描述或状态无效")
	}
	if s.StartDate != nil && !validDate(*s.StartDate) || s.DueDate != nil && !validDate(*s.DueDate) {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "项目日期无效")
	}
	if s.StartDate != nil && s.DueDate != nil && *s.DueDate < *s.StartDate {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "项目截止日期不能早于开始日期")
	}
	return nil
}

func sameProjectSnapshot(a, b projectSnapshot) bool {
	return a.Name == b.Name && a.Description == b.Description && a.Status == b.Status && ptrString(a.StartDate) == ptrString(b.StartDate) && ptrString(a.DueDate) == ptrString(b.DueDate)
}

func projectForUpdate(ctx context.Context, tx *sql.Tx, teamID, projectID string) (project, int, error) {
	var result project
	var start, due sql.NullTime
	var version int
	err := tx.QueryRowContext(ctx, `SELECT HEX(id),HEX(team_id),name,description,status,start_date,due_date,created_at,version FROM projects WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL FOR UPDATE`, projectID, teamID).Scan(&result.ID, &result.TeamID, &result.Name, &result.Description, &result.Status, &start, &due, &result.CreatedAt, &version)
	if err != nil {
		return project{}, 0, err
	}
	result.StartDate = date(start)
	result.DueDate = date(due)
	return result, version, nil
}

func (s *Server) projectProposals(w http.ResponseWriter, r *http.Request, u User, teamID, role string, parts []string) error {
	if len(parts) == 3 && r.Method == http.MethodGet {
		rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(pc.id),HEX(pc.project_id),p.name,HEX(pc.proposed_by),pc.reason,pc.impact_summary,pc.before_snapshot,pc.proposed_snapshot,pc.status,pc.project_version,pc.created_at,pc.resolved_at FROM project_change_proposals pc JOIN projects p ON p.id=pc.project_id WHERE pc.team_id=UNHEX(?) ORDER BY pc.created_at DESC LIMIT 100`, teamID)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := make([]map[string]any, 0)
		for rows.Next() {
			var id, projectID, projectName, author, reason, impact, status string
			var beforeRaw, proposedRaw []byte
			var version int
			var created time.Time
			var resolved sql.NullTime
			if err = rows.Scan(&id, &projectID, &projectName, &author, &reason, &impact, &beforeRaw, &proposedRaw, &status, &version, &created, &resolved); err != nil {
				return err
			}
			var before, proposed projectSnapshot
			if err = json.Unmarshal(beforeRaw, &before); err != nil {
				return err
			}
			if err = json.Unmarshal(proposedRaw, &proposed); err != nil {
				return err
			}
			out = append(out, map[string]any{"id": id, "projectId": projectID, "projectName": projectName, "proposedBy": author, "reason": reason, "impactSummary": impact, "before": before, "proposed": proposed, "status": status, "projectVersion": version, "createdAt": created, "resolvedAt": resolved.Time})
		}
		write(w, http.StatusOK, out)
		return rows.Err()
	}
	if len(parts) == 3 && r.Method == http.MethodPost {
		var input projectProposalInput
		if err := body(w, r, &input); err != nil {
			return err
		}
		input.Reason = strings.TrimSpace(input.Reason)
		if !validID(input.ProjectID) || len([]rune(input.Reason)) < 5 || len([]rune(input.Reason)) > 5000 || len([]rune(input.ImpactSummary)) < 3 || len([]rune(input.ImpactSummary)) > 5000 {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请填写有效项目、变更原因和影响说明")
		}
		return s.createProjectProposal(w, r, u, teamID, input, "project.proposal.create", "项目变更待审批")
	}
	if len(parts) == 5 && parts[4] == "resubmit" && r.Method == http.MethodPost {
		return s.resubmitProjectProposal(w, r, u, teamID, parts[3])
	}
	if len(parts) == 5 && parts[4] == "decision" && r.Method == http.MethodPost {
		if err := admin(role); err != nil {
			return err
		}
		return s.decideProjectProposal(w, r, u, teamID, parts[3])
	}
	return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}

func (s *Server) createProjectProposal(w http.ResponseWriter, r *http.Request, u User, teamID string, input projectProposalInput, auditAction, notificationTitle string) error {
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	proposalID, err := s.createProjectProposalTx(r.Context(), tx, u, teamID, input, auditAction, notificationTitle)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusCreated, map[string]string{"id": proposalID, "status": "pending"})
	return nil
}

func (s *Server) createProjectProposalTx(ctx context.Context, tx *sql.Tx, u User, teamID string, input projectProposalInput, auditAction, notificationTitle string) (string, error) {
	if _, err := activeTeamRoleTx(ctx, tx, teamID, u); err != nil {
		return "", err
	}
	current, version, err := projectForUpdate(ctx, tx, teamID, input.ProjectID)
	if err == sql.ErrNoRows {
		return "", bad(http.StatusNotFound, "NOT_FOUND", "项目不存在")
	}
	if err != nil {
		return "", err
	}
	before := projectSnapshotFrom(current)
	proposed := applyProjectSnapshot(before, input)
	if err = validateProjectSnapshot(proposed); err != nil {
		return "", err
	}
	if sameProjectSnapshot(before, proposed) {
		return "", bad(http.StatusBadRequest, "NO_PROJECT_CHANGES", "至少修改一个项目字段")
	}
	beforeJSON, _ := json.Marshal(before)
	proposedJSON, _ := json.Marshal(proposed)
	proposalID := id()
	_, err = tx.ExecContext(ctx, `INSERT INTO project_change_proposals(id,team_id,project_id,project_version,proposed_by,source_ref,reason,impact_summary,before_snapshot,proposed_snapshot) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),?,UNHEX(?),IF(?='',NULL,UNHEX(?)),?,?,?,?)`, proposalID, teamID, input.ProjectID, version, u.ID, input.SourceRef, input.SourceRef, input.Reason, input.ImpactSummary, beforeJSON, proposedJSON)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			return "", bad(http.StatusConflict, "PROPOSAL_PENDING", "该项目已有待处理变更提案")
		}
		return "", err
	}
	if err = addAudit(tx, u.ID, teamID, auditAction, "project_proposal", proposalID); err != nil {
		return "", err
	}
	if err = notifyProjectAdmins(tx, teamID, u.ID, notificationTitle, current.Name, proposalID); err != nil {
		return "", err
	}
	if input.AgentProvider != "" {
		safeArgs, _ := json.Marshal(map[string]any{"projectId": input.ProjectID, "changedFields": projectChangedFields(before, proposed)})
		resultJSON, _ := json.Marshal(map[string]string{"proposalId": proposalID, "status": "pending"})
		runID, callID := id(), id()
		contextSummary, _ := json.Marshal(map[string]string{"purpose": "tool_execution", "tool": "propose_project_change", "approval": "pending"})
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_runs(id,user_id,team_id,conversation_id,provider,model,status,context_summary,started_at,finished_at) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,?,'succeeded',?,UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`, runID, u.ID, teamID, input.SourceRef, input.AgentProvider, input.AgentModel, contextSummary); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_tool_calls(id,run_id,user_id,team_id,tool_name,argument_summary,result_summary,approval_status,status) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),'propose_project_change',?,?, 'pending','succeeded')`, callID, runID, u.ID, teamID, safeArgs, resultJSON); err != nil {
			return "", err
		}
		if err = addAudit(tx, u.ID, teamID, "agent.tool.execute", "agent_tool_call", callID); err != nil {
			return "", err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(id,team_id,event_type,aggregate_type,aggregate_id,payload) VALUES(UNHEX(?),UNHEX(?),'project.proposal.created','project_proposal',UNHEX(?),JSON_OBJECT('projectId',?,'status','pending'))`, id(), teamID, proposalID, input.ProjectID)
	if err != nil {
		return "", err
	}
	return proposalID, nil
}

func notifyProjectAdmins(tx *sql.Tx, teamID, actorID, title, body, proposalID string) error {
	rows, err := tx.Query(`SELECT HEX(user_id) FROM team_memberships WHERE team_id=UNHEX(?) AND role IN ('team_owner','team_admin') AND status='active'`, teamID)
	if err != nil {
		return err
	}
	var recipients []string
	for rows.Next() {
		var recipient string
		if err = rows.Scan(&recipient); err != nil {
			rows.Close()
			return err
		}
		if recipient != actorID {
			recipients = append(recipients, recipient)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err = notifyResource(tx, teamID, recipient, "approval", title, body, "project_proposal", proposalID); err != nil {
			return err
		}
	}
	return nil
}

func projectChangedFields(before, after projectSnapshot) []string {
	changed := make([]string, 0, 5)
	if before.Name != after.Name {
		changed = append(changed, "name")
	}
	if before.Description != after.Description {
		changed = append(changed, "description")
	}
	if before.Status != after.Status {
		changed = append(changed, "status")
	}
	if ptrString(before.StartDate) != ptrString(after.StartDate) {
		changed = append(changed, "startDate")
	}
	if ptrString(before.DueDate) != ptrString(after.DueDate) {
		changed = append(changed, "dueDate")
	}
	return changed
}

func (s *Server) resubmitProjectProposal(w http.ResponseWriter, r *http.Request, u User, teamID, proposalID string) error {
	if !validID(proposalID) {
		return bad(http.StatusNotFound, "NOT_FOUND", "提案不存在")
	}
	var input projectProposalInput
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if len([]rune(input.Reason)) < 5 || len([]rune(input.Reason)) > 5000 || len([]rune(input.ImpactSummary)) < 3 || len([]rune(input.ImpactSummary)) > 5000 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请填写变更原因和影响说明")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = activeTeamRoleTx(r.Context(), tx, teamID, u); err != nil {
		return err
	}
	var projectID, author, status string
	var version int
	var previous []byte
	err = tx.QueryRowContext(r.Context(), `SELECT HEX(project_id),HEX(proposed_by),status,project_version,proposed_snapshot FROM project_change_proposals WHERE id=UNHEX(?) AND team_id=UNHEX(?) FOR UPDATE`, proposalID, teamID).Scan(&projectID, &author, &status, &version, &previous)
	if err == sql.ErrNoRows {
		return bad(http.StatusNotFound, "NOT_FOUND", "提案不存在")
	}
	if err != nil {
		return err
	}
	if author != u.ID {
		return bad(http.StatusForbidden, "PROPOSAL_OWNER_REQUIRED", "只有提交人可以补充提案")
	}
	if status != "needs_info" {
		return bad(http.StatusConflict, "PROPOSAL_STATE_CONFLICT", "提案当前不需要补充信息")
	}
	current, currentVersion, err := projectForUpdate(r.Context(), tx, teamID, projectID)
	if err == sql.ErrNoRows {
		return bad(http.StatusConflict, "VERSION_CONFLICT", "项目已不存在，请重新提交")
	}
	if err != nil {
		return err
	}
	if currentVersion != version {
		return bad(http.StatusConflict, "VERSION_CONFLICT", "项目已变化，请重新提交提案")
	}
	var proposed projectSnapshot
	if err = json.Unmarshal(previous, &proposed); err != nil {
		return err
	}
	proposed = applyProjectSnapshot(proposed, input)
	if err = validateProjectSnapshot(proposed); err != nil {
		return err
	}
	if sameProjectSnapshot(projectSnapshotFrom(current), proposed) {
		return bad(http.StatusBadRequest, "NO_PROJECT_CHANGES", "至少修改一个项目字段")
	}
	encoded, _ := json.Marshal(proposed)
	_, err = tx.ExecContext(r.Context(), `UPDATE project_change_proposals SET status='pending',resolved_at=NULL,reason=?,impact_summary=?,proposed_snapshot=? WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, input.Reason, input.ImpactSummary, encoded, proposalID, teamID)
	if err != nil {
		return err
	}
	if err = addAudit(tx, u.ID, teamID, "project.proposal.resubmit", "project_proposal", proposalID); err != nil {
		return err
	}
	if err = notifyProjectAdmins(tx, teamID, u.ID, "项目变更已补充", current.Name, proposalID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]string{"status": "pending"})
	return nil
}

func (s *Server) decideProjectProposal(w http.ResponseWriter, r *http.Request, u User, teamID, proposalID string) error {
	if !validID(proposalID) {
		return bad(http.StatusNotFound, "NOT_FOUND", "提案不存在")
	}
	var input struct {
		Decision    string  `json:"decision"`
		Reason      string  `json:"reason"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Status      *string `json:"status"`
		StartDate   *string `json:"startDate"`
		DueDate     *string `json:"dueDate"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if !in(input.Decision, "approved", "edited_approved", "rejected", "needs_info") || len([]rune(input.Reason)) > 5000 || (input.Decision == "rejected" || input.Decision == "needs_info") && len([]rune(input.Reason)) < 5 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "审批决定或说明无效")
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requireTeamAdminTx(r.Context(), tx, teamID, u); err != nil {
		return err
	}
	var projectID, author, status string
	var version int
	var beforeRaw, proposedRaw []byte
	err = tx.QueryRowContext(r.Context(), `SELECT HEX(project_id),HEX(proposed_by),status,project_version,before_snapshot,proposed_snapshot FROM project_change_proposals WHERE id=UNHEX(?) AND team_id=UNHEX(?) FOR UPDATE`, proposalID, teamID).Scan(&projectID, &author, &status, &version, &beforeRaw, &proposedRaw)
	if err == sql.ErrNoRows {
		return bad(http.StatusNotFound, "NOT_FOUND", "提案不存在")
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return bad(http.StatusConflict, "PROPOSAL_RESOLVED", "提案已处理")
	}
	_, currentVersion, err := projectForUpdate(r.Context(), tx, teamID, projectID)
	if err == sql.ErrNoRows {
		return bad(http.StatusConflict, "PROJECT_CHANGED", "项目已不存在")
	}
	if err != nil {
		return err
	}
	if currentVersion != version {
		if _, err = tx.ExecContext(r.Context(), `UPDATE project_change_proposals SET status='stale',resolved_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND status='pending'`, proposalID, teamID); err != nil {
			return err
		}
		if err = addAudit(tx, u.ID, teamID, "project.proposal.stale", "project_proposal", proposalID); err != nil {
			return err
		}
		if author != u.ID {
			if err = notifyResource(tx, teamID, author, "project", "项目提案已过期", "项目在审批前发生变化，请重新提交提案", "project_proposal", proposalID); err != nil {
				return err
			}
		}
		stalePayload, _ := json.Marshal(map[string]string{"proposalId": proposalID, "status": "stale"})
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO outbox_events(id,team_id,event_type,aggregate_type,aggregate_id,payload) VALUES(UNHEX(?),UNHEX(?),'project.proposal.stale','project_proposal',UNHEX(?),?)`, id(), teamID, proposalID, stalePayload); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return bad(http.StatusConflict, "VERSION_CONFLICT", "项目已变化，该提案已标记过期，请重新提交")
	}
	var proposed projectSnapshot
	if err = json.Unmarshal(proposedRaw, &proposed); err != nil {
		return err
	}
	if input.Decision == "edited_approved" {
		proposed = applyProjectSnapshot(proposed, projectProposalInput{Name: input.Name, Description: input.Description, Status: input.Status, StartDate: input.StartDate, DueDate: input.DueDate})
	}
	if input.Decision == "approved" || input.Decision == "edited_approved" {
		if err = validateProjectSnapshot(proposed); err != nil {
			return err
		}
	}
	newStatus := input.Decision
	if input.Decision == "edited_approved" {
		newStatus = "approved"
	}
	afterRaw, _ := json.Marshal(proposed)
	if input.Decision == "approved" || input.Decision == "edited_approved" {
		result, updateErr := tx.ExecContext(r.Context(), `UPDATE projects SET name=?,description=?,status=?,start_date=?,due_date=?,version=version+1 WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND version=? AND deleted_at IS NULL`, proposed.Name, proposed.Description, proposed.Status, nullableDate(ptrString(proposed.StartDate)), nullableDate(ptrString(proposed.DueDate)), projectID, teamID, version)
		if updateErr != nil {
			return updateErr
		}
		changed, _ := result.RowsAffected()
		if changed != 1 {
			return bad(http.StatusConflict, "VERSION_CONFLICT", "项目已被其他人修改")
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE project_change_proposals SET status=?,resolved_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND status='pending'`, newStatus, proposalID, teamID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO project_change_approvals(id,team_id,proposal_id,approver_id,decision,reason,before_snapshot,after_snapshot) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,?,?,?)`, id(), teamID, proposalID, u.ID, input.Decision, input.Reason, beforeRaw, afterRaw); err != nil {
		return err
	}
	if err = addAudit(tx, u.ID, teamID, "project.proposal."+input.Decision, "project_proposal", proposalID); err != nil {
		return err
	}
	if author != u.ID {
		if err = notifyResource(tx, teamID, author, "project", "项目变更审批结果", input.Decision, "project_proposal", proposalID); err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]string{"proposalId": proposalID, "decision": input.Decision})
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO outbox_events(id,team_id,event_type,aggregate_type,aggregate_id,payload) VALUES(UNHEX(?),UNHEX(?),'project.proposal.decided','project_proposal',UNHEX(?),?)`, id(), teamID, proposalID, payload); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]string{"status": newStatus})
	return nil
}
