package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type membershipSnapshot struct {
	Role    string `json:"role"`
	Status  string `json:"status"`
	Version uint64 `json:"version"`
}

type memberProposalInput struct {
	MemberID      string  `json:"memberId"`
	Role          *string `json:"role,omitempty"`
	Status        *string `json:"status,omitempty"`
	Reason        string  `json:"reason"`
	ImpactSummary string  `json:"impactSummary"`
	SourceRef     string  `json:"-"`
	AgentProvider string  `json:"-"`
	AgentModel    string  `json:"-"`
}

func validateMembershipSnapshot(s membershipSnapshot) error {
	if !in(s.Role, "member", "team_admin") || !in(s.Status, "active", "suspended") {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "成员提案只能设置普通成员或管理员，以及有效或停用状态")
	}
	return nil
}

func (s *Server) memberProposals(w http.ResponseWriter, r *http.Request, u User, teamID, role string, parts []string) error {
	if len(parts) == 3 && r.Method == http.MethodGet {
		rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(p.id),HEX(p.target_user_id),target.display_name,HEX(p.proposed_by),author.display_name,p.before_snapshot,p.proposed_snapshot,p.reason,p.impact_summary,p.status,p.created_at,p.resolved_at FROM member_change_proposals p JOIN users target ON target.id=p.target_user_id JOIN users author ON author.id=p.proposed_by WHERE p.team_id=UNHEX(?) ORDER BY p.created_at DESC LIMIT 100`, teamID)
		if err != nil {
			return err
		}
		defer rows.Close()
		out := make([]map[string]any, 0)
		for rows.Next() {
			var itemID, memberID, memberName, proposedBy, proposedByName, reason, impact, status string
			var beforeRaw, proposedRaw []byte
			var created time.Time
			var resolved sql.NullTime
			if err = rows.Scan(&itemID, &memberID, &memberName, &proposedBy, &proposedByName, &beforeRaw, &proposedRaw, &reason, &impact, &status, &created, &resolved); err != nil {
				return err
			}
			var before, proposed membershipSnapshot
			if err = json.Unmarshal(beforeRaw, &before); err != nil {
				return err
			}
			if err = json.Unmarshal(proposedRaw, &proposed); err != nil {
				return err
			}
			out = append(out, map[string]any{"id": itemID, "memberId": memberID, "memberName": memberName, "proposedBy": proposedBy, "proposedByName": proposedByName, "before": before, "proposed": proposed, "reason": reason, "impactSummary": impact, "status": status, "createdAt": created, "resolvedAt": resolved.Time})
		}
		write(w, http.StatusOK, out)
		return rows.Err()
	}
	if len(parts) == 3 && r.Method == http.MethodPost {
		var input memberProposalInput
		if err := body(w, r, &input); err != nil {
			return err
		}
		if err := validateMemberProposalInput(input); err != nil {
			return err
		}
		return s.createMemberProposal(w, r, u, teamID, input, "member.proposal.create", "成员身份变更待审批")
	}
	if len(parts) == 5 && parts[4] == "resubmit" && r.Method == http.MethodPost {
		return s.resubmitMemberProposal(w, r, u, teamID, parts[3])
	}
	if len(parts) == 5 && parts[4] == "decision" && r.Method == http.MethodPost {
		if err := admin(role); err != nil {
			return err
		}
		return s.decideMemberProposal(w, r, u, teamID, parts[3], role)
	}
	return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
}

func validateMemberProposalInput(input memberProposalInput) error {
	input.Reason = strings.TrimSpace(input.Reason)
	input.ImpactSummary = strings.TrimSpace(input.ImpactSummary)
	if !validID(input.MemberID) || len([]rune(input.Reason)) < 5 || len([]rune(input.Reason)) > 5000 || len([]rune(input.ImpactSummary)) < 3 || len([]rune(input.ImpactSummary)) > 5000 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请填写有效成员、至少 5 个字的原因和影响说明")
	}
	if input.Role == nil && input.Status == nil {
		return bad(http.StatusBadRequest, "NO_MEMBER_CHANGES", "至少提出一项成员角色或状态变更")
	}
	if input.Role != nil && !in(*input.Role, "member", "team_admin") {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "成员角色无效；所有权转移必须由所有者直接处理")
	}
	if input.Status != nil && !in(*input.Status, "active", "suspended") {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "成员状态无效")
	}
	return nil
}

func (s *Server) createMemberProposal(w http.ResponseWriter, r *http.Request, u User, teamID string, input memberProposalInput, action, title string) error {
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	proposalID, err := s.createMemberProposalTx(r.Context(), tx, u, teamID, input, action, title)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusCreated, map[string]string{"id": proposalID, "status": "pending"})
	return nil
}

func (s *Server) createMemberProposalTx(ctx context.Context, tx *sql.Tx, u User, teamID string, input memberProposalInput, action, title string) (string, error) {
	if _, err := activeTeamRoleTx(ctx, tx, teamID, u); err != nil {
		return "", err
	}
	if input.MemberID == u.ID {
		return "", bad(http.StatusBadRequest, "SELF_CHANGE_DENIED", "不能为自己的成员身份提交变更提案")
	}
	var current membershipSnapshot
	err := tx.QueryRowContext(ctx, `SELECT role,status,version FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND status IN ('active','suspended') FOR UPDATE`, teamID, input.MemberID).Scan(&current.Role, &current.Status, &current.Version)
	if err == sql.ErrNoRows {
		return "", bad(http.StatusNotFound, "NOT_FOUND", "团队成员不存在")
	}
	if err != nil {
		return "", err
	}
	if current.Role == "team_owner" {
		return "", bad(http.StatusConflict, "OWNER_TRANSFER_REQUIRED", "所有者身份只能通过所有权转移流程变更")
	}
	proposed := current
	if input.Role != nil {
		proposed.Role = *input.Role
	}
	if input.Status != nil {
		proposed.Status = *input.Status
	}
	if err = validateMembershipSnapshot(proposed); err != nil {
		return "", err
	}
	if proposed == current {
		return "", bad(http.StatusBadRequest, "NO_MEMBER_CHANGES", "提案内容与当前成员身份相同")
	}
	beforeJSON, _ := json.Marshal(current)
	proposedJSON, _ := json.Marshal(proposed)
	proposalID := id()
	_, err = tx.ExecContext(ctx, `INSERT INTO member_change_proposals(id,team_id,target_user_id,proposed_by,source_ref,before_snapshot,proposed_snapshot,reason,impact_summary) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),IF(?='',NULL,UNHEX(?)),?,?,?,?)`, proposalID, teamID, input.MemberID, u.ID, input.SourceRef, input.SourceRef, beforeJSON, proposedJSON, strings.TrimSpace(input.Reason), strings.TrimSpace(input.ImpactSummary))
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			return "", bad(http.StatusConflict, "PROPOSAL_PENDING", "该成员已有待处理身份变更提案")
		}
		return "", err
	}
	if err = addAudit(tx, u.ID, teamID, action, "member_proposal", proposalID); err != nil {
		return "", err
	}
	if err = notifyMemberProposalAdmins(tx, teamID, u.ID, input.MemberID, title, proposalID); err != nil {
		return "", err
	}
	if input.AgentProvider != "" {
		safeArgs, _ := json.Marshal(map[string]any{"memberId": input.MemberID, "proposedRole": proposed.Role, "proposedStatus": proposed.Status})
		resultJSON, _ := json.Marshal(map[string]string{"proposalId": proposalID, "status": "pending"})
		contextSummary, _ := json.Marshal(map[string]string{"purpose": "tool_execution", "tool": "propose_member_change", "approval": "pending"})
		runID, callID := id(), id()
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_runs(id,user_id,team_id,conversation_id,provider,model,status,context_summary,started_at,finished_at) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,?,'succeeded',?,UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`, runID, u.ID, teamID, input.SourceRef, input.AgentProvider, input.AgentModel, contextSummary); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_tool_calls(id,run_id,user_id,team_id,tool_name,argument_summary,result_summary,approval_status,status) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),'propose_member_change',?,?, 'pending','succeeded')`, callID, runID, u.ID, teamID, safeArgs, resultJSON); err != nil {
			return "", err
		}
		if err = addAudit(tx, u.ID, teamID, "agent.tool.execute", "agent_tool_call", callID); err != nil {
			return "", err
		}
	}
	payload, _ := json.Marshal(map[string]string{"proposalId": proposalID, "memberId": input.MemberID, "status": "pending"})
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(id,team_id,event_type,aggregate_type,aggregate_id,payload) VALUES(UNHEX(?),UNHEX(?),'member.proposal.created','member_proposal',UNHEX(?),?)`, id(), teamID, proposalID, payload); err != nil {
		return "", err
	}
	return proposalID, nil
}

func notifyMemberProposalAdmins(tx *sql.Tx, teamID, actorID, targetID, title, proposalID string) error {
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
		if recipient != actorID && recipient != targetID {
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
		if err = notifyResource(tx, teamID, recipient, "approval", title, "智能体或成员提交了一项待审批的成员身份变更", "member_proposal", proposalID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) resubmitMemberProposal(w http.ResponseWriter, r *http.Request, u User, teamID, proposalID string) error {
	if !validID(proposalID) {
		return bad(http.StatusNotFound, "NOT_FOUND", "提案不存在")
	}
	var input struct {
		Role          *string `json:"role"`
		Status        *string `json:"status"`
		Reason        string  `json:"reason"`
		ImpactSummary string  `json:"impactSummary"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	if err := validateMemberProposalInput(memberProposalInput{MemberID: id(), Role: input.Role, Status: input.Status, Reason: input.Reason, ImpactSummary: input.ImpactSummary}); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = activeTeamRoleTx(r.Context(), tx, teamID, u); err != nil {
		return err
	}
	var targetID, author, status string
	var beforeRaw, priorProposedRaw []byte
	err = tx.QueryRowContext(r.Context(), `SELECT HEX(target_user_id),HEX(proposed_by),status,before_snapshot,proposed_snapshot FROM member_change_proposals WHERE id=UNHEX(?) AND team_id=UNHEX(?) FOR UPDATE`, proposalID, teamID).Scan(&targetID, &author, &status, &beforeRaw, &priorProposedRaw)
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
	var current membershipSnapshot
	err = tx.QueryRowContext(r.Context(), `SELECT role,status,version FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) FOR UPDATE`, teamID, targetID).Scan(&current.Role, &current.Status, &current.Version)
	if err == sql.ErrNoRows {
		return bad(http.StatusConflict, "MEMBER_CHANGED", "成员已不在团队，请重新提交提案")
	}
	if err != nil {
		return err
	}
	var before membershipSnapshot
	if err = json.Unmarshal(beforeRaw, &before); err != nil {
		return err
	}
	if current != before {
		return bad(http.StatusConflict, "MEMBER_CHANGED", "成员身份已变化，请重新提交提案")
	}
	proposed := current
	if input.Role != nil {
		proposed.Role = *input.Role
	}
	if input.Status != nil {
		proposed.Status = *input.Status
	}
	if err = validateMembershipSnapshot(proposed); err != nil {
		return err
	}
	if proposed == current {
		return bad(http.StatusBadRequest, "NO_MEMBER_CHANGES", "补充后的提案与当前成员身份相同")
	}
	proposedRaw, _ := json.Marshal(proposed)
	if _, err = tx.ExecContext(r.Context(), `UPDATE member_change_proposals SET proposed_snapshot=?,reason=?,impact_summary=?,status='pending',resolved_at=NULL WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND status='needs_info'`, proposedRaw, strings.TrimSpace(input.Reason), strings.TrimSpace(input.ImpactSummary), proposalID, teamID); err != nil {
		return err
	}
	if err = addAudit(tx, u.ID, teamID, "member.proposal.resubmit", "member_proposal", proposalID); err != nil {
		return err
	}
	if err = notifyMemberProposalAdmins(tx, teamID, u.ID, targetID, "成员身份提案已补充", proposalID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO outbox_events(id,team_id,event_type,aggregate_type,aggregate_id,payload) VALUES(UNHEX(?),UNHEX(?),'member.proposal.resubmitted','member_proposal',UNHEX(?),JSON_OBJECT('status','pending'))`, id(), teamID, proposalID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]string{"status": "pending"})
	return nil
}

func (s *Server) decideMemberProposal(w http.ResponseWriter, r *http.Request, u User, teamID, proposalID, actorRole string) error {
	if !validID(proposalID) {
		return bad(http.StatusNotFound, "NOT_FOUND", "提案不存在")
	}
	var input struct {
		Decision string  `json:"decision"`
		Reason   string  `json:"reason"`
		Role     *string `json:"role"`
		Status   *string `json:"status"`
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
	currentRole, roleErr := activeTeamRoleTx(r.Context(), tx, teamID, u)
	if roleErr != nil {
		return roleErr
	}
	if currentRole != actorRole {
		return bad(http.StatusForbidden, "TEAM_ROLE_CHANGED", "你的团队权限已变化，请刷新后重试")
	}
	if err = requireTeamAdminTx(r.Context(), tx, teamID, u); err != nil {
		return err
	}
	var targetID, author, status string
	var beforeRaw, proposedRaw []byte
	err = tx.QueryRowContext(r.Context(), `SELECT HEX(target_user_id),HEX(proposed_by),status,before_snapshot,proposed_snapshot FROM member_change_proposals WHERE id=UNHEX(?) AND team_id=UNHEX(?) FOR UPDATE`, proposalID, teamID).Scan(&targetID, &author, &status, &beforeRaw, &proposedRaw)
	if err == sql.ErrNoRows {
		return bad(http.StatusNotFound, "NOT_FOUND", "提案不存在")
	}
	if err != nil {
		return err
	}
	if author == u.ID {
		return bad(http.StatusForbidden, "PROPOSAL_AUTHOR_CANNOT_APPROVE", "提案提交人不能审批自己的成员变更")
	}
	if status != "pending" {
		return bad(http.StatusConflict, "PROPOSAL_RESOLVED", "提案已处理")
	}
	var before, proposed membershipSnapshot
	if err = json.Unmarshal(beforeRaw, &before); err != nil {
		return err
	}
	if err = json.Unmarshal(proposedRaw, &proposed); err != nil {
		return err
	}
	var current membershipSnapshot
	err = tx.QueryRowContext(r.Context(), `SELECT role,status,version FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) FOR UPDATE`, teamID, targetID).Scan(&current.Role, &current.Status, &current.Version)
	if err == sql.ErrNoRows {
		current = membershipSnapshot{Role: "", Status: "left"}
	} else if err != nil {
		return err
	}
	if current != before {
		if _, err = tx.ExecContext(r.Context(), `UPDATE member_change_proposals SET status='stale',resolved_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND status='pending'`, proposalID, teamID); err != nil {
			return err
		}
		if err = addAudit(tx, u.ID, teamID, "member.proposal.stale", "member_proposal", proposalID); err != nil {
			return err
		}
		if err = notifyResource(tx, teamID, author, "member", "成员提案已过期", "目标成员身份在审批前发生变化，请重新提交提案", "member_proposal", proposalID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO outbox_events(id,team_id,event_type,aggregate_type,aggregate_id,payload) VALUES(UNHEX(?),UNHEX(?),'member.proposal.stale','member_proposal',UNHEX(?),JSON_OBJECT('status','stale'))`, id(), teamID, proposalID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		return bad(http.StatusConflict, "MEMBER_CHANGED", "成员身份已变化，该提案已过期")
	}
	if input.Decision == "edited_approved" {
		if input.Role != nil {
			proposed.Role = *input.Role
		}
		if input.Status != nil {
			proposed.Status = *input.Status
		}
	}
	if input.Decision == "approved" || input.Decision == "edited_approved" {
		if err = validateMembershipSnapshot(proposed); err != nil {
			return err
		}
		if currentRole != "team_owner" && (current.Role != "member" || proposed.Role == "team_admin") {
			return bad(http.StatusForbidden, "TEAM_OWNER_REQUIRED", "管理员只能审批普通成员的非管理员身份变更；任命管理员须由所有者审批")
		}
		if proposed.Role == current.Role && proposed.Status == current.Status {
			return bad(http.StatusBadRequest, "NO_MEMBER_CHANGES", "审批结果与当前成员身份相同")
		}
		res, updateErr := tx.ExecContext(r.Context(), `UPDATE team_memberships SET role=?,status=?,version=version+1 WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND role=? AND status=? AND version=?`, proposed.Role, proposed.Status, teamID, targetID, current.Role, current.Status, current.Version)
		if updateErr != nil {
			return updateErr
		}
		changed, _ := res.RowsAffected()
		if changed != 1 {
			return bad(http.StatusConflict, "MEMBER_CHANGED", "成员身份已被其他人修改")
		}
		proposed.Version = current.Version + 1
	}
	newStatus := input.Decision
	if input.Decision == "edited_approved" {
		newStatus = "approved"
	}
	afterRaw, _ := json.Marshal(proposed)
	if _, err = tx.ExecContext(r.Context(), `UPDATE member_change_proposals SET status=?,resolved_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND status='pending'`, newStatus, proposalID, teamID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO member_change_approvals(id,team_id,proposal_id,approver_id,decision,reason,before_snapshot,after_snapshot) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,?,?,?)`, id(), teamID, proposalID, u.ID, input.Decision, input.Reason, beforeRaw, afterRaw); err != nil {
		return err
	}
	if err = addAudit(tx, u.ID, teamID, "member.proposal."+input.Decision, "member_proposal", proposalID); err != nil {
		return err
	}
	if err = notifyResource(tx, teamID, author, "member", "成员身份提案审批结果", input.Decision, "member_proposal", proposalID); err != nil {
		return err
	}
	if targetID != author {
		if err = notifyResource(tx, teamID, targetID, "member", "你的团队身份已调整", "团队管理员已处理一项涉及你成员身份的提案", "member_proposal", proposalID); err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]string{"proposalId": proposalID, "decision": input.Decision})
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO outbox_events(id,team_id,event_type,aggregate_type,aggregate_id,payload) VALUES(UNHEX(?),UNHEX(?),'member.proposal.decided','member_proposal',UNHEX(?),?)`, id(), teamID, proposalID, payload); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]string{"status": newStatus})
	return nil
}

func (s *Server) createAgentMemberProposal(ctx context.Context, u User, teamID, conversationID string, input memberProposalInput) (map[string]any, []byte, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	proposalID, err := s.createMemberProposalTx(ctx, tx, u, teamID, input, "agent.member.proposal.create", "智能体提交了成员身份变更提案")
	if err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	safeArgs, _ := json.Marshal(map[string]any{"memberId": input.MemberID, "role": input.Role, "status": input.Status})
	return map[string]any{"proposalId": proposalID, "status": "pending", "memberId": input.MemberID}, safeArgs, nil
}
