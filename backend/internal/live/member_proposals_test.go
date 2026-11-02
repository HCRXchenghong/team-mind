package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestMemberProposalApprovalBoundaryAndStaleState(t *testing.T) {
	if strings.TrimSpace(os.Getenv("MYSQL_DSN")) == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	authorID, reviewerID, ownerID, targetID, staleTargetID, concurrentTargetID, agentTargetID := id(), id(), id(), id(), id(), id(), id()
	teamID, conversationID := id(), id()
	t.Cleanup(func() {
		server.DB.Exec(`DELETE FROM notification_deliveries WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM notifications WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM outbox_events WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM member_change_proposals WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM agent_tool_calls WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM agent_runs WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, conversationID)
		server.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		for _, userID := range []string{authorID, reviewerID, ownerID, targetID, staleTargetID, concurrentTargetID, agentTargetID} {
			server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		}
		server.DB.Close()
	})
	for _, item := range []struct{ id, role string }{{authorID, "member"}, {reviewerID, "team_admin"}, {ownerID, "team_owner"}, {targetID, "member"}, {staleTargetID, "member"}, {concurrentTargetID, "member"}, {agentTargetID, "member"}} {
		if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, item.id, "member-proposal-"+item.id+"@example.invalid", "Member Proposal Test", "not-a-login"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Member Proposal Team',?,'active',UNHEX(?),'{}')`, teamID, "member-proposal-"+teamID, authorID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, role string }{{authorID, "member"}, {reviewerID, "team_admin"}, {ownerID, "team_owner"}, {targetID, "member"}, {staleTargetID, "member"}, {concurrentTargetID, "member"}, {agentTargetID, "member"}} {
		if _, err = server.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),?,'active',UTC_TIMESTAMP(3))`, teamID, item.id, item.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,team_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'team_context','Member proposal integration')`, conversationID, authorID, teamID); err != nil {
		t.Fatal(err)
	}
	adminRole := "team_admin"
	input := memberProposalInput{MemberID: strings.ToUpper(targetID), Role: &adminRole, Reason: "Coordinate operations", ImpactSummary: "Needs management access for approved team duties", SourceRef: strings.ToUpper(conversationID), AgentProvider: "integration-test", AgentModel: "fixture"}
	result, _, err := server.createAgentMemberProposal(context.Background(), User{ID: strings.ToUpper(authorID)}, strings.ToUpper(teamID), strings.ToUpper(conversationID), input)
	if err != nil {
		t.Fatal(err)
	}
	proposalID := result["proposalId"].(string)
	var targetRole, proposalStatus, approvalStatus string
	if err = server.DB.QueryRow(`SELECT role FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, targetID).Scan(&targetRole); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT status FROM member_change_proposals WHERE id=UNHEX(?)`, proposalID).Scan(&proposalStatus); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT approval_status FROM agent_tool_calls WHERE team_id=UNHEX(?) AND tool_name='propose_member_change'`, teamID).Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	if targetRole != "member" || proposalStatus != "pending" || approvalStatus != "pending" {
		t.Fatalf("proposal modified authority before approval: target=%s proposal=%s tool=%s", targetRole, proposalStatus, approvalStatus)
	}
	admin := User{ID: strings.ToUpper(reviewerID)}
	adminApprove := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"approved","reason":"Approved"}`))
	adminResponse := httptest.NewRecorder()
	err = server.decideMemberProposal(adminResponse, adminApprove, admin, strings.ToUpper(teamID), proposalID, "team_admin")
	var apiErr failure
	if !errors.As(err, &apiErr) || apiErr.code != "TEAM_OWNER_REQUIRED" {
		t.Fatalf("admin promotion approval should require owner, got %v", err)
	}
	if err = server.DB.QueryRow(`SELECT status FROM member_change_proposals WHERE id=UNHEX(?)`, proposalID).Scan(&proposalStatus); err != nil || proposalStatus != "pending" {
		t.Fatalf("owner-only rejection must leave proposal pending: status=%q err=%v", proposalStatus, err)
	}

	needsInfoRequest := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"needs_info","reason":"Please clarify the scope"}`))
	needsInfoResponse := httptest.NewRecorder()
	if err = server.decideMemberProposal(needsInfoResponse, needsInfoRequest, admin, strings.ToUpper(teamID), proposalID, "team_admin"); err != nil {
		t.Fatal(err)
	}
	resubmitRequest := httptest.NewRequest(http.MethodPost, "/resubmit", strings.NewReader(`{"role":"team_admin","status":"active","reason":"Clarified by requester","impactSummary":"Operations lead will cover on-call coordination"}`))
	resubmitResponse := httptest.NewRecorder()
	if err = server.resubmitMemberProposal(resubmitResponse, resubmitRequest, User{ID: strings.ToUpper(authorID)}, strings.ToUpper(teamID), proposalID); err != nil {
		t.Fatal(err)
	}
	ownerApprove := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"approved","reason":"Owner approved access"}`))
	ownerResponse := httptest.NewRecorder()
	if err = server.decideMemberProposal(ownerResponse, ownerApprove, User{ID: strings.ToUpper(ownerID)}, strings.ToUpper(teamID), proposalID, "team_owner"); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT role,status FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, targetID).Scan(&targetRole, &proposalStatus); err != nil {
		t.Fatal(err)
	}
	if targetRole != "team_admin" || proposalStatus != "active" {
		t.Fatalf("approved proposal not atomically applied: role=%s status=%s", targetRole, proposalStatus)
	}
	if err = server.DB.QueryRow(`SELECT status FROM member_change_proposals WHERE id=UNHEX(?)`, proposalID).Scan(&proposalStatus); err != nil || proposalStatus != "approved" {
		t.Fatalf("approved proposal status=%q err=%v", proposalStatus, err)
	}
	var approvalCount, auditCount, outboxCount int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM member_change_approvals WHERE proposal_id=UNHEX(?)`, proposalID).Scan(&approvalCount); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE team_id=UNHEX(?) AND resource_type='member_proposal' AND resource_id=UNHEX(?)`, teamID, proposalID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE team_id=UNHEX(?) AND aggregate_type='member_proposal' AND aggregate_id=UNHEX(?)`, teamID, proposalID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if approvalCount != 2 || auditCount < 4 || outboxCount != 4 {
		t.Fatalf("approval effects not recorded consistently: approvals=%d audit=%d outbox=%d", approvalCount, auditCount, outboxCount)
	}
	duplicateRequest := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"approved","reason":"Duplicate click"}`))
	duplicateResponse := httptest.NewRecorder()
	err = server.decideMemberProposal(duplicateResponse, duplicateRequest, User{ID: strings.ToUpper(ownerID)}, strings.ToUpper(teamID), proposalID, "team_owner")
	if !errors.As(err, &apiErr) || apiErr.code != "PROPOSAL_RESOLVED" {
		t.Fatalf("duplicate decision should be rejected, got %v", err)
	}

	deactivate := "suspended"
	activate := "active"
	stale, _, err := server.createAgentMemberProposal(context.Background(), User{ID: strings.ToUpper(authorID)}, strings.ToUpper(teamID), strings.ToUpper(conversationID), memberProposalInput{MemberID: strings.ToUpper(staleTargetID), Status: &deactivate, Reason: "Pause suspended account", ImpactSummary: "Block access while team investigates"})
	if err != nil {
		t.Fatal(err)
	}
	staleID := stale["proposalId"].(string)
	if _, err = server.DB.Exec(`UPDATE team_memberships SET status='suspended',version=version+1 WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, staleTargetID); err != nil {
		t.Fatal(err)
	}
	staleApprove := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"approved","reason":"Approve stale change"}`))
	staleResponse := httptest.NewRecorder()
	err = server.decideMemberProposal(staleResponse, staleApprove, User{ID: strings.ToUpper(ownerID)}, strings.ToUpper(teamID), staleID, "team_owner")
	if !errors.As(err, &apiErr) || apiErr.code != "MEMBER_CHANGED" {
		t.Fatalf("changed membership should invalidate proposal, got %v", err)
	}
	if err = server.DB.QueryRow(`SELECT status FROM member_change_proposals WHERE id=UNHEX(?)`, staleID).Scan(&proposalStatus); err != nil || proposalStatus != "stale" {
		t.Fatalf("stale proposal state=%q err=%v", proposalStatus, err)
	}

	versionOnlyProposal, _, err := server.createAgentMemberProposal(context.Background(), User{ID: strings.ToUpper(authorID)}, strings.ToUpper(teamID), strings.ToUpper(conversationID), memberProposalInput{MemberID: strings.ToUpper(staleTargetID), Status: &activate, Reason: "Restore access after review", ImpactSummary: "The account is cleared to resume work"})
	if err != nil {
		t.Fatal(err)
	}
	versionOnlyID := versionOnlyProposal["proposalId"].(string)
	if _, err = server.DB.Exec(`UPDATE team_memberships SET version=version+1 WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, staleTargetID); err != nil {
		t.Fatal(err)
	}
	versionOnlyApprove := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"approved","reason":"Reject old membership revision"}`))
	versionOnlyResponse := httptest.NewRecorder()
	err = server.decideMemberProposal(versionOnlyResponse, versionOnlyApprove, User{ID: strings.ToUpper(ownerID)}, strings.ToUpper(teamID), versionOnlyID, "team_owner")
	if !errors.As(err, &apiErr) || apiErr.code != "MEMBER_CHANGED" {
		t.Fatalf("leave/rejoin-style membership version change should invalidate proposal, got %v", err)
	}
	if err = server.DB.QueryRow(`SELECT status FROM member_change_proposals WHERE id=UNHEX(?)`, versionOnlyID).Scan(&proposalStatus); err != nil || proposalStatus != "stale" {
		t.Fatalf("version-only stale proposal state=%q err=%v", proposalStatus, err)
	}

	adminProposal, _, err := server.createAgentMemberProposal(context.Background(), User{ID: strings.ToUpper(reviewerID)}, strings.ToUpper(teamID), strings.ToUpper(conversationID), memberProposalInput{MemberID: strings.ToUpper(staleTargetID), Status: &activate, Reason: "Restore account after review", ImpactSummary: "The investigation is complete and access can resume"})
	if err != nil {
		t.Fatal(err)
	}
	adminProposalID := adminProposal["proposalId"].(string)
	selfDecision := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"approved","reason":"Self approval attempt"}`))
	selfResponse := httptest.NewRecorder()
	err = server.decideMemberProposal(selfResponse, selfDecision, User{ID: strings.ToUpper(reviewerID)}, strings.ToUpper(teamID), adminProposalID, "team_admin")
	if !errors.As(err, &apiErr) || apiErr.code != "PROPOSAL_AUTHOR_CANNOT_APPROVE" {
		t.Fatalf("author must not approve own proposal, got %v", err)
	}
	ownerDecision := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"approved","reason":"Owner approved restoration"}`))
	ownerResult := httptest.NewRecorder()
	if err = server.decideMemberProposal(ownerResult, ownerDecision, User{ID: strings.ToUpper(ownerID)}, strings.ToUpper(teamID), adminProposalID, "team_owner"); err != nil {
		t.Fatal(err)
	}

	suspend := "suspended"
	concurrentProposal, _, err := server.createAgentMemberProposal(context.Background(), User{ID: strings.ToUpper(authorID)}, strings.ToUpper(teamID), strings.ToUpper(conversationID), memberProposalInput{MemberID: strings.ToUpper(concurrentTargetID), Status: &suspend, Reason: "Temporarily pause access", ImpactSummary: "Review account access after a security report"})
	if err != nil {
		t.Fatal(err)
	}
	concurrentID := concurrentProposal["proposalId"].(string)
	decisionBodies := []string{`{"decision":"approved","reason":"Owner approved"}`, `{"decision":"approved","reason":"Admin approved"}`}
	decisionUsers := []struct{ id, role string }{{ownerID, "team_owner"}, {reviewerID, "team_admin"}}
	errs := make([]error, len(decisionUsers))
	var wg sync.WaitGroup
	for i := range decisionUsers {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			request := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(decisionBodies[index]))
			response := httptest.NewRecorder()
			errs[index] = server.decideMemberProposal(response, request, User{ID: strings.ToUpper(decisionUsers[index].id)}, strings.ToUpper(teamID), concurrentID, decisionUsers[index].role)
		}(i)
	}
	wg.Wait()
	successCount, resolvedCount := 0, 0
	for _, decideErr := range errs {
		if decideErr == nil {
			successCount++
			continue
		}
		if errors.As(decideErr, &apiErr) && apiErr.code == "PROPOSAL_RESOLVED" {
			resolvedCount++
			continue
		}
		t.Fatalf("unexpected concurrent decision result: %v", decideErr)
	}
	if successCount != 1 || resolvedCount != 1 {
		t.Fatalf("concurrent review must commit exactly once: success=%d resolved=%d", successCount, resolvedCount)
	}
	var concurrentStatus string
	var concurrentApprovals int
	if err = server.DB.QueryRow(`SELECT status FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, concurrentTargetID).Scan(&concurrentStatus); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM member_change_approvals WHERE proposal_id=UNHEX(?) AND decision='approved'`, concurrentID).Scan(&concurrentApprovals); err != nil {
		t.Fatal(err)
	}
	if concurrentStatus != "suspended" || concurrentApprovals != 1 {
		t.Fatalf("concurrent review persisted unexpected outcome: membership=%s approvals=%d", concurrentStatus, concurrentApprovals)
	}

	var modelCalls int
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ToolChoice string            `json:"tool_choice"`
			Messages   []json.RawMessage `json:"messages"`
			Tools      []struct {
				Function struct {
					Name       string         `json:"name"`
					Parameters map[string]any `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode member proposal model request: %v", err)
		}
		modelCalls++
		w.Header().Set("Content-Type", "application/json")
		if request.ToolChoice == "none" {
			_, _ = w.Write([]byte(`{"model":"member-fixture","choices":[{"message":{"content":"Submitted for review."}}]}`))
			return
		}
		if len(request.Tools) != 6 || request.Tools[0].Function.Name != "list_team_members" || request.Tools[5].Function.Name != "propose_member_change" {
			t.Errorf("model did not receive member proposal capability: %+v", request.Tools)
		}
		if modelCalls == 1 {
			arguments, _ := json.Marshal(map[string]any{})
			response, _ := json.Marshal(map[string]any{"model": "member-fixture", "choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{"id": "member-list", "type": "function", "function": map[string]any{"name": "list_team_members", "arguments": string(arguments)}}}}}}})
			_, _ = w.Write(response)
			return
		}
		messagesJSON, _ := json.Marshal(request.Messages)
		if !strings.Contains(string(messagesJSON), "Member Proposal Test") || strings.Contains(string(messagesJSON), "@example.invalid") {
			t.Errorf("member lookup result omitted names or exposed email addresses: %s", messagesJSON)
		}
		memberSchema, _ := json.Marshal(request.Tools[0].Function.Parameters)
		if strings.Contains(string(memberSchema), "email") {
			t.Error("member lookup schema unexpectedly exposed email data")
		}
		arguments, _ := json.Marshal(map[string]string{"memberId": agentTargetID, "status": "suspended", "reason": "Pause access during investigation", "impactSummary": "Temporarily suspend this account while reviewing the security report"})
		response, _ := json.Marshal(map[string]any{"model": "member-fixture", "choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{"id": "member-proposal", "type": "function", "function": map[string]any{"name": "propose_member_change", "arguments": string(arguments)}}}}}}})
		_, _ = w.Write(response)
	}))
	defer model.Close()
	t.Setenv("AI_PROVIDER", "openai_compatible")
	t.Setenv("AI_BASE_URL", model.URL)
	t.Setenv("AI_API_KEY", "fixture-only-key")
	t.Setenv("AI_MODEL", "member-fixture")
	modelRequest := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(`{"instruction":"Request a temporary member suspension","confirmTeam":true}`))
	modelResponse := httptest.NewRecorder()
	if err = server.executeAgent(modelResponse, modelRequest, User{ID: strings.ToUpper(authorID)}, strings.ToUpper(conversationID), sql.NullString{String: strings.ToUpper(teamID), Valid: true}); err != nil {
		t.Fatalf("execute model-selected member proposal: %v (%s)", err, modelResponse.Body.String())
	}
	if modelResponse.Code != http.StatusOK || modelCalls != 3 {
		t.Fatalf("model member proposal response status=%d calls=%d body=%s", modelResponse.Code, modelCalls, modelResponse.Body.String())
	}
	var agentProposalID string
	if err = server.DB.QueryRow(`SELECT HEX(id) FROM member_change_proposals WHERE team_id=UNHEX(?) AND target_user_id=UNHEX(?) AND status='pending'`, teamID, agentTargetID).Scan(&agentProposalID); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT status FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, agentTargetID).Scan(&concurrentStatus); err != nil {
		t.Fatal(err)
	}
	if concurrentStatus != "active" {
		t.Fatalf("model proposal wrote membership before approval: %s", concurrentStatus)
	}
	modelDecision := httptest.NewRequest(http.MethodPost, "/decision", strings.NewReader(`{"decision":"approved","reason":"Admin approved after review"}`))
	modelDecisionResponse := httptest.NewRecorder()
	if err = server.decideMemberProposal(modelDecisionResponse, modelDecision, User{ID: strings.ToUpper(reviewerID)}, strings.ToUpper(teamID), agentProposalID, "team_admin"); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT status FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, agentTargetID).Scan(&concurrentStatus); err != nil || concurrentStatus != "suspended" {
		t.Fatalf("admin approval did not apply model-proposed suspension: status=%q err=%v", concurrentStatus, err)
	}

	err = server.createMemberProposal(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/member-proposals", strings.NewReader(`{}`)), User{ID: strings.ToUpper(authorID)}, strings.ToUpper(teamID), memberProposalInput{MemberID: strings.ToUpper(ownerID), Status: &deactivate, Reason: "Change owner status", ImpactSummary: "Owner access change required"}, "member.proposal.create", "Member change")
	if !errors.As(err, &apiErr) || apiErr.code != "OWNER_TRANSFER_REQUIRED" {
		t.Fatalf("owner change should use ownership transfer route, got %v", err)
	}
	err = server.createMemberProposal(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/member-proposals", strings.NewReader(`{}`)), User{ID: strings.ToUpper(authorID)}, strings.ToUpper(teamID), memberProposalInput{MemberID: strings.ToUpper(authorID), Status: &deactivate, Reason: "Change own status", ImpactSummary: "Self change"}, "member.proposal.create", "Member change")
	if !errors.As(err, &apiErr) || apiErr.code != "SELF_CHANGE_DENIED" {
		t.Fatalf("self change should be refused, got %v", err)
	}
}
