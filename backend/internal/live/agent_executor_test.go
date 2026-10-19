package live

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAgentResultSummaryReflectsSandboxPersistence(t *testing.T) {
	for _, test := range []struct {
		name      string
		persisted bool
	}{
		{name: "temporary", persisted: false},
		{name: "saved to private workspace", persisted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			summary := agentResultSummary("run_shell", map[string]any{
				"exitCode":  0,
				"stdout":    "sensitive output must not enter audit",
				"files":     map[string]string{"result.txt": "sensitive file content"},
				"temporary": !test.persisted,
				"persisted": test.persisted,
			})
			if summary["temporary"] != !test.persisted || summary["persisted"] != test.persisted {
				t.Fatalf("summary does not reflect persistence state: %#v", summary)
			}
			encoded, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "sensitive") || strings.Contains(string(encoded), "result.txt") {
				t.Fatalf("audit summary exposed sandbox data: %s", encoded)
			}
		})
	}
}

func TestModelSelectedWriteToolCreatesOnlyPendingProposal(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	userID, reviewerID, teamID, projectID, taskID, conversationID := id(), id(), id(), id(), id(), id()
	t.Cleanup(func() {
		server.DB.Exec(`DELETE FROM notification_deliveries WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM notifications WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM outbox_events WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM plan_change_approvals WHERE proposal_id IN (SELECT id FROM plan_change_proposals WHERE team_id=UNHEX(?))`, teamID)
		server.DB.Exec(`DELETE FROM plan_change_proposals WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM agent_tool_calls WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM agent_runs WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, conversationID)
		server.DB.Exec(`DELETE FROM task_plan_versions WHERE task_id=UNHEX(?)`, taskID)
		server.DB.Exec(`DELETE FROM tasks WHERE id=UNHEX(?)`, taskID)
		server.DB.Exec(`DELETE FROM projects WHERE id=UNHEX(?)`, projectID)
		server.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, reviewerID)
		server.DB.Close()
	})
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "agent-proposal-"+userID+"@example.invalid", "Agent Proposal Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, reviewerID, "agent-reviewer-"+reviewerID+"@example.invalid", "Agent Proposal Reviewer", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Agent Proposal Team',?,'active',UNHEX(?),'{}')`, teamID, "agent-proposal-"+teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'member','active',UTC_TIMESTAMP(3))`, teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_admin','active',UTC_TIMESTAMP(3))`, teamID, reviewerID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO projects(id,team_id,name,description,status,created_by) VALUES(UNHEX(?),UNHEX(?),'Agent project','','active',UNHEX(?))`, projectID, teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO tasks(id,team_id,project_id,title,description,created_by,assignee_id) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'Original task','',UNHEX(?),UNHEX(?))`, taskID, teamID, projectID, userID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,team_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'team_context','Agent approval test')`, conversationID, userID, teamID); err != nil {
		t.Fatal(err)
	}
	var calls int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []json.RawMessage `json:"messages"`
			Tools    []struct {
				Function struct {
					Name       string `json:"name"`
					Parameters struct {
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
		}
		count := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			if len(request.Tools) != 6 || request.Tools[0].Function.Name != "list_team_members" || request.Tools[1].Function.Name != "list_team_tasks" || request.Tools[2].Function.Name != "list_team_projects" || request.Tools[3].Function.Name != "propose_task_change" || request.Tools[4].Function.Name != "propose_project_change" || request.Tools[5].Function.Name != "propose_member_change" {
				t.Errorf("model received unexpected capability list: %+v", request.Tools)
			}
			if _, ok := request.Tools[3].Function.Parameters.Properties["status"]; !ok {
				t.Error("task proposal schema does not expose an approval-gated status update")
			}
			if _, ok := request.Tools[3].Function.Parameters.Properties["progress"]; !ok {
				t.Error("task proposal schema does not expose an approval-gated progress update")
			}
			_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"tool_calls":[{"id":"call-tasks","type":"function","function":{"name":"list_team_tasks","arguments":"{}"}}]}}]}`)
			return
		}
		encodedMessages, _ := json.Marshal(request.Messages)
		if !strings.Contains(string(encodedMessages), "call-tasks") || !strings.Contains(string(encodedMessages), "Original task") {
			t.Errorf("second model turn did not receive the prior task query result: %s", encodedMessages)
		}
		if count == 2 {
			_, _ = fmt.Fprintf(w, `{"model":"test-model","choices":[{"message":{"tool_calls":[{"id":"call-proposal","type":"function","function":{"name":"propose_task_change","arguments":%q}}]}}]}`, fmt.Sprintf(`{"taskId":%q,"title":"Proposed title","status":"done","progress":90,"reason":"Clarify scope","impactSummary":"Delivery scope review required"}`, taskID))
			return
		}
		if count != 3 || !strings.Contains(string(encodedMessages), "call-proposal") {
			t.Errorf("unexpected model round or missing proposal tool result: calls=%d messages=%s", count, encodedMessages)
		}
		_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"I found the task and submitted its proposed change for team review."}}]}`)
	}))
	defer model.Close()
	t.Setenv("AI_PROVIDER", "openai_compatible")
	t.Setenv("AI_BASE_URL", model.URL)
	t.Setenv("AI_API_KEY", "test-only-key")
	t.Setenv("AI_MODEL", "test-model")
	request := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(`{"instruction":"Propose a clearer task title","confirmTeam":true}`))
	response := httptest.NewRecorder()
	err = server.executeAgent(response, request, User{ID: strings.ToUpper(userID), Role: "member"}, strings.ToUpper(conversationID), sql.NullString{String: strings.ToUpper(teamID), Valid: true})
	if err != nil {
		t.Fatalf("execute model-selected proposal: %v (%d %s)", err, response.Code, response.Body.String())
	}
	if response.Code != http.StatusOK || atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("response status=%d model calls=%d body=%s", response.Code, atomic.LoadInt32(&calls), response.Body.String())
	}
	var result struct {
		Data struct {
			Tool   string          `json:"tool"`
			Result map[string]any  `json:"result"`
			Steps  []agentToolStep `json:"steps"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Tool != "propose_task_change" || result.Data.Result["status"] != "pending" || len(result.Data.Steps) != 2 || result.Data.Steps[0].Tool != "list_team_tasks" || result.Data.Steps[1].Tool != "propose_task_change" {
		t.Fatalf("unexpected tool result: %+v", result.Data)
	}
	var currentTitle, currentTaskStatus, proposalStatus, approvalStatus string
	var currentProgress int
	if err = server.DB.QueryRow(`SELECT title,status,progress FROM tasks WHERE id=UNHEX(?)`, taskID).Scan(&currentTitle, &currentTaskStatus, &currentProgress); err != nil {
		t.Fatal(err)
	}
	var proposalID string
	var proposedSnapshot []byte
	if err = server.DB.QueryRow(`SELECT HEX(id),proposed_snapshot FROM plan_change_proposals WHERE task_id=UNHEX(?)`, taskID).Scan(&proposalID, &proposedSnapshot); err != nil {
		t.Fatal(err)
	}
	var proposed taskPlanSnapshot
	if err = json.Unmarshal(proposedSnapshot, &proposed); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT status FROM plan_change_proposals WHERE task_id=UNHEX(?)`, taskID).Scan(&proposalStatus); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT approval_status FROM agent_tool_calls WHERE user_id=UNHEX(?) AND tool_name='propose_task_change'`, userID).Scan(&approvalStatus); err != nil {
		t.Fatal(err)
	}
	var persistedMessages, auditCount, toolCallCount int
	var workflowSummary []byte
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM conversation_messages WHERE conversation_id=UNHEX(?)`, conversationID).Scan(&persistedMessages); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE team_id=UNHEX(?) AND action='agent.proposal.create'`, teamID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM agent_tool_calls WHERE team_id=UNHEX(?) AND status='succeeded'`, teamID).Scan(&toolCallCount); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT context_summary FROM agent_runs WHERE conversation_id=UNHEX(?) AND JSON_UNQUOTE(JSON_EXTRACT(context_summary,'$.purpose'))='agent_workflow'`, conversationID).Scan(&workflowSummary); err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		ToolCount int      `json:"toolCount"`
		Tools     []string `json:"tools"`
	}
	if err = json.Unmarshal(workflowSummary, &workflow); err != nil {
		t.Fatal(err)
	}
	if currentTitle != "Original task" || currentTaskStatus != "todo" || currentProgress != 0 || proposed.Status != "done" || proposed.Progress != 90 || proposalStatus != "pending" || approvalStatus != "pending" || persistedMessages != 2 || auditCount != 1 || toolCallCount != 2 || workflow.ToolCount != 2 || len(workflow.Tools) != 2 || workflow.Tools[0] != "list_team_tasks" || workflow.Tools[1] != "propose_task_change" {
		t.Fatalf("proposal boundary broken: task=%q/%s/%d proposed=%s/%d proposal=%q approval=%q messages=%d audits=%d toolCalls=%d workflow=%s", currentTitle, currentTaskStatus, currentProgress, proposed.Status, proposed.Progress, proposalStatus, approvalStatus, persistedMessages, auditCount, toolCallCount, workflowSummary)
	}
	decisionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID+"/plan-proposals/"+proposalID+"/decision", strings.NewReader(`{"decision":"approved","reason":"Approved by reviewer"}`))
	decisionResponse := httptest.NewRecorder()
	if err = server.proposals(decisionResponse, decisionRequest, User{ID: reviewerID, Role: "team_admin"}, teamID, "team_admin", []string{"teams", teamID, "plan-proposals", proposalID, "decision"}); err != nil {
		t.Fatalf("approve task status/progress proposal: %v (%d %s)", err, decisionResponse.Code, decisionResponse.Body.String())
	}
	if decisionResponse.Code != http.StatusOK {
		t.Fatalf("approval status=%d body=%s", decisionResponse.Code, decisionResponse.Body.String())
	}
	if err = server.DB.QueryRow(`SELECT title,status,progress FROM tasks WHERE id=UNHEX(?)`, taskID).Scan(&currentTitle, &currentTaskStatus, &currentProgress); err != nil {
		t.Fatal(err)
	}
	if currentTitle != "Proposed title" || currentTaskStatus != "done" || currentProgress != 90 {
		t.Fatalf("approved plan did not atomically update task status/progress: %q/%s/%d", currentTitle, currentTaskStatus, currentProgress)
	}
	var planVersion, approvalCount, notificationCount, auditCountAfter, outboxCount int
	var historyJSON []byte
	if err = server.DB.QueryRow(`SELECT version,snapshot FROM task_plan_versions WHERE task_id=UNHEX(?) ORDER BY version DESC LIMIT 1`, taskID).Scan(&planVersion, &historyJSON); err != nil {
		t.Fatal(err)
	}
	var history taskPlanSnapshot
	if err = json.Unmarshal(historyJSON, &history); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM plan_change_approvals WHERE proposal_id=UNHEX(?) AND decision='approved'`, proposalID).Scan(&approvalCount); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE recipient_user_id=UNHEX(?) AND resource_id=UNHEX(?)`, userID, proposalID).Scan(&notificationCount); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE team_id=UNHEX(?) AND action='proposal.approved' AND resource_id=UNHEX(?)`, teamID, proposalID).Scan(&auditCountAfter); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE team_id=UNHEX(?) AND aggregate_id=UNHEX(?) AND event_type='plan.decided'`, teamID, proposalID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if planVersion != 2 || history.Status != "done" || history.Progress != 90 || approvalCount != 1 || notificationCount != 1 || auditCountAfter != 1 || outboxCount != 1 {
		t.Fatalf("approval transaction did not persist all effects: version=%d history=%s/%d approvals=%d notifications=%d audits=%d outbox=%d", planVersion, history.Status, history.Progress, approvalCount, notificationCount, auditCountAfter, outboxCount)
	}
}

func TestModelSelectedProjectProposalStaysPendingUntilTeamAdminApproves(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	memberID, adminID, teamID, projectID, conversationID := id(), id(), id(), id(), id()
	t.Cleanup(func() {
		server.DB.Exec(`DELETE FROM notification_deliveries WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM notifications WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM outbox_events WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM project_change_approvals WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM project_change_proposals WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM agent_tool_calls WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM agent_runs WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, conversationID)
		server.DB.Exec(`DELETE FROM projects WHERE id=UNHEX(?)`, projectID)
		server.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?))`, memberID, adminID)
		server.DB.Close()
	})
	for _, user := range []struct{ id, email, name string }{{memberID, "agent-project-member-" + memberID + "@example.invalid", "Project Proposer"}, {adminID, "agent-project-admin-" + adminID + "@example.invalid", "Project Reviewer"}} {
		if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, user.id, user.email, user.name, "not-a-login"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Project Proposal Team',?,'active',UNHEX(?),'{}')`, teamID, "project-proposal-"+teamID, memberID); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, role string }{{memberID, "member"}, {adminID, "team_admin"}} {
		if _, err = server.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),?,'active',UTC_TIMESTAMP(3))`, teamID, item.id, item.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO projects(id,team_id,name,description,status,created_by) VALUES(UNHEX(?),UNHEX(?),'Original project','Original description','planning',UNHEX(?))`, projectID, teamID, memberID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,team_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'team_context','Project proposal test')`, conversationID, memberID, teamID); err != nil {
		t.Fatal(err)
	}
	var modelCalls int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []json.RawMessage `json:"messages"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
		}
		call := atomic.AddInt32(&modelCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			if len(request.Tools) != 6 || request.Tools[2].Function.Name != "list_team_projects" || request.Tools[4].Function.Name != "propose_project_change" || request.Tools[5].Function.Name != "propose_member_change" {
				t.Errorf("model did not receive project read/proposal tools: %+v", request.Tools)
			}
			_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"tool_calls":[{"id":"call-project-list","type":"function","function":{"name":"list_team_projects","arguments":"{}"}}]}}]}`)
			return
		}
		encoded, _ := json.Marshal(request.Messages)
		if call == 2 {
			if !strings.Contains(string(encoded), "Original project") || !strings.Contains(string(encoded), strings.ToUpper(projectID)) {
				t.Errorf("project query result missing from next model turn: %s", encoded)
			}
			_, _ = fmt.Fprintf(w, `{"model":"test-model","choices":[{"message":{"tool_calls":[{"id":"call-project-proposal","type":"function","function":{"name":"propose_project_change","arguments":%q}}]}}]}`, fmt.Sprintf(`{"projectId":%q,"name":"Revised project","status":"active","reason":"Reflect approved delivery scope","impactSummary":"This affects team roadmap dates"}`, projectID))
			return
		}
		if call != 3 || !strings.Contains(string(encoded), "call-project-proposal") {
			t.Errorf("unexpected final model round: %d %s", call, encoded)
		}
		_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"I submitted the project change for administrator review."}}]}`)
	}))
	defer model.Close()
	t.Setenv("AI_PROVIDER", "openai_compatible")
	t.Setenv("AI_BASE_URL", model.URL)
	t.Setenv("AI_API_KEY", "test-only-key")
	t.Setenv("AI_MODEL", "test-model")
	request := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(`{"instruction":"Update the current project to active and rename it","confirmTeam":true}`))
	response := httptest.NewRecorder()
	err = server.executeAgent(response, request, User{ID: strings.ToUpper(memberID), Role: "member"}, strings.ToUpper(conversationID), sql.NullString{String: strings.ToUpper(teamID), Valid: true})
	if err != nil {
		t.Fatalf("execute project proposal: %v (%d %s)", err, response.Code, response.Body.String())
	}
	if response.Code != http.StatusOK || atomic.LoadInt32(&modelCalls) != 3 {
		t.Fatalf("status=%d modelCalls=%d body=%s", response.Code, modelCalls, response.Body.String())
	}
	var currentName, currentStatus string
	var currentVersion int
	if err = server.DB.QueryRow(`SELECT name,status,version FROM projects WHERE id=UNHEX(?)`, projectID).Scan(&currentName, &currentStatus, &currentVersion); err != nil {
		t.Fatal(err)
	}
	var proposalID, proposalStatus string
	if err = server.DB.QueryRow(`SELECT HEX(id),status FROM project_change_proposals WHERE project_id=UNHEX(?)`, projectID).Scan(&proposalID, &proposalStatus); err != nil {
		t.Fatal(err)
	}
	var toolCalls, adminNotifications int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM agent_tool_calls WHERE team_id=UNHEX(?) AND tool_name='propose_project_change' AND approval_status='pending' AND status='succeeded'`, teamID).Scan(&toolCalls); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE team_id=UNHEX(?) AND recipient_user_id=UNHEX(?) AND resource_type='project_proposal' AND resource_id=UNHEX(?)`, teamID, adminID, proposalID).Scan(&adminNotifications); err != nil {
		t.Fatal(err)
	}
	if currentName != "Original project" || currentStatus != "planning" || currentVersion != 1 || proposalStatus != "pending" || toolCalls != 1 || adminNotifications != 1 {
		t.Fatalf("proposal bypassed approval or audit path: current=%s/%s/v%d status=%s toolCalls=%d notifications=%d", currentName, currentStatus, currentVersion, proposalStatus, toolCalls, adminNotifications)
	}
	memberDecision := httptest.NewRecorder()
	memberRequest := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID+"/project-proposals/"+proposalID+"/decision", strings.NewReader(`{"decision":"approved","reason":"approve"}`))
	memberErr := server.projectProposals(memberDecision, memberRequest, User{ID: strings.ToUpper(memberID), Role: "member"}, strings.ToUpper(teamID), "member", []string{"teams", strings.ToUpper(teamID), "project-proposals", proposalID, "decision"})
	var denied failure
	if !errors.As(memberErr, &denied) || denied.status != http.StatusForbidden {
		t.Fatalf("member was allowed to approve: %v", memberErr)
	}
	needInfoRequest := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID+"/project-proposals/"+proposalID+"/decision", strings.NewReader(`{"decision":"needs_info","reason":"Please clarify delivery impact"}`))
	needInfoResponse := httptest.NewRecorder()
	if err = server.projectProposals(needInfoResponse, needInfoRequest, User{ID: strings.ToUpper(adminID), Role: "team_admin"}, strings.ToUpper(teamID), "team_admin", []string{"teams", strings.ToUpper(teamID), "project-proposals", proposalID, "decision"}); err != nil {
		t.Fatalf("request for more information failed: %v", err)
	}
	if needInfoResponse.Code != http.StatusOK {
		t.Fatalf("needs-info status=%d body=%s", needInfoResponse.Code, needInfoResponse.Body.String())
	}
	var afterNeedsInfo string
	if err = server.DB.QueryRow(`SELECT status FROM project_change_proposals WHERE id=UNHEX(?)`, proposalID).Scan(&afterNeedsInfo); err != nil {
		t.Fatal(err)
	}
	if afterNeedsInfo != "needs_info" {
		t.Fatalf("needs-info state not persisted: %s", afterNeedsInfo)
	}
	resubmitRequest := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID+"/project-proposals/"+proposalID+"/resubmit", strings.NewReader(`{"projectId":"`+projectID+`","name":"Revised project","status":"active","reason":"Clarify delivery scope after reviewer request","impactSummary":"This affects team roadmap dates"}`))
	resubmitResponse := httptest.NewRecorder()
	if err = server.projectProposals(resubmitResponse, resubmitRequest, User{ID: strings.ToUpper(memberID), Role: "member"}, strings.ToUpper(teamID), "member", []string{"teams", strings.ToUpper(teamID), "project-proposals", proposalID, "resubmit"}); err != nil {
		t.Fatalf("resubmission failed: %v", err)
	}
	if resubmitResponse.Code != http.StatusOK {
		t.Fatalf("resubmit status=%d body=%s", resubmitResponse.Code, resubmitResponse.Body.String())
	}
	adminRequest := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID+"/project-proposals/"+proposalID+"/decision", strings.NewReader(`{"decision":"approved","reason":"approved after review"}`))
	adminResponse := httptest.NewRecorder()
	if err = server.projectProposals(adminResponse, adminRequest, User{ID: strings.ToUpper(adminID), Role: "team_admin"}, strings.ToUpper(teamID), "team_admin", []string{"teams", strings.ToUpper(teamID), "project-proposals", proposalID, "decision"}); err != nil {
		t.Fatalf("admin approval failed: %v", err)
	}
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("admin approval status=%d body=%s", adminResponse.Code, adminResponse.Body.String())
	}
	if err = server.DB.QueryRow(`SELECT name,status,version FROM projects WHERE id=UNHEX(?)`, projectID).Scan(&currentName, &currentStatus, &currentVersion); err != nil {
		t.Fatal(err)
	}
	if currentName != "Revised project" || currentStatus != "active" || currentVersion != 2 {
		t.Fatalf("approved project not applied atomically: %s/%s/v%d", currentName, currentStatus, currentVersion)
	}
	var decisionHistory int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM project_change_approvals WHERE proposal_id=UNHEX(?)`, proposalID).Scan(&decisionHistory); err != nil {
		t.Fatal(err)
	}
	if decisionHistory != 2 {
		t.Fatalf("needs-info and approval decisions not both recorded: %d", decisionHistory)
	}
	duplicateResponse := httptest.NewRecorder()
	duplicateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID+"/project-proposals/"+proposalID+"/decision", strings.NewReader(`{"decision":"approved","reason":"duplicate approval"}`))
	duplicateErr := server.projectProposals(duplicateResponse, duplicateRequest, User{ID: strings.ToUpper(adminID), Role: "team_admin"}, strings.ToUpper(teamID), "team_admin", []string{"teams", strings.ToUpper(teamID), "project-proposals", proposalID, "decision"})
	if !errors.As(duplicateErr, &denied) || denied.status != http.StatusConflict {
		t.Fatalf("duplicate approval was not rejected: %v", duplicateErr)
	}
	secondProposalRequest := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID+"/project-proposals", strings.NewReader(`{"projectId":"`+projectID+`","name":"Stale proposal","reason":"Prepare next project update","impactSummary":"This requires a project timeline review"}`))
	secondProposalResponse := httptest.NewRecorder()
	if err = server.projectProposals(secondProposalResponse, secondProposalRequest, User{ID: strings.ToUpper(memberID), Role: "member"}, strings.ToUpper(teamID), "member", []string{"teams", strings.ToUpper(teamID), "project-proposals"}); err != nil {
		t.Fatalf("create second proposal failed: %v", err)
	}
	var staleProposalID string
	var secondEnvelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(secondProposalResponse.Body.Bytes(), &secondEnvelope); err != nil {
		t.Fatal(err)
	}
	staleProposalID = secondEnvelope.Data.ID
	if staleProposalID == "" {
		t.Fatalf("second proposal response missing id: %s", secondProposalResponse.Body.String())
	}
	if _, err = server.DB.Exec(`UPDATE projects SET version=version+1 WHERE id=UNHEX(?)`, projectID); err != nil {
		t.Fatal(err)
	}
	staleRequest := httptest.NewRequest(http.MethodPost, "/api/v1/teams/"+teamID+"/project-proposals/"+staleProposalID+"/decision", strings.NewReader(`{"decision":"approved","reason":"approve latest proposal"}`))
	staleResponse := httptest.NewRecorder()
	staleErr := server.projectProposals(staleResponse, staleRequest, User{ID: strings.ToUpper(adminID), Role: "team_admin"}, strings.ToUpper(teamID), "team_admin", []string{"teams", strings.ToUpper(teamID), "project-proposals", staleProposalID, "decision"})
	if !errors.As(staleErr, &denied) || denied.status != http.StatusConflict {
		t.Fatalf("stale version was not rejected: %v", staleErr)
	}
	var staleStatus string
	if err = server.DB.QueryRow(`SELECT status FROM project_change_proposals WHERE id=UNHEX(?)`, staleProposalID).Scan(&staleStatus); err != nil {
		t.Fatal(err)
	}
	if staleStatus != "stale" {
		t.Fatalf("version-conflicted proposal not marked stale: %s", staleStatus)
	}
}

func TestModelSelectedTeamKnowledgeCandidateRequiresConsentAndKeepsSourcePrivate(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	actorID, reviewerID, memberID, teamID, conversationID := id(), id(), id(), id(), id()
	t.Cleanup(func() {
		server.DB.Exec(`DELETE FROM notification_deliveries WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM notifications WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM agent_tool_calls WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM agent_runs WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM team_memories WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, conversationID)
		server.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		server.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?),UNHEX(?))`, actorID, reviewerID, memberID)
		server.DB.Close()
	})
	for _, entry := range []struct{ id, email, name string }{
		{actorID, "agent-memory-actor-" + actorID + "@example.invalid", "Agent Memory Actor"},
		{reviewerID, "agent-memory-reviewer-" + reviewerID + "@example.invalid", "Agent Memory Reviewer"},
		{memberID, "agent-memory-member-" + memberID + "@example.invalid", "Agent Memory Member"},
	} {
		if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, entry.id, entry.email, entry.name, "not-a-login"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Agent Memory Team',?,'active',UNHEX(?),'{}')`, teamID, "agent-memory-"+teamID, actorID); err != nil {
		t.Fatal(err)
	}
	for _, membership := range []struct{ userID, role string }{{actorID, "team_admin"}, {reviewerID, "team_owner"}, {memberID, "member"}} {
		if _, err = server.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),?,'active',UTC_TIMESTAMP(3))`, teamID, membership.userID, membership.role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,team_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'team_context','Agent memory test')`, conversationID, actorID, teamID); err != nil {
		t.Fatal(err)
	}
	var modelCalls int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&modelCalls, 1) == 1 {
			if len(request.Tools) != 7 || request.Tools[6].Function.Name != "create_team_memory_candidate" {
				t.Errorf("authorized team admin got unexpected tools: %+v", request.Tools)
			}
			_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"tool_calls":[{"id":"call-memory","type":"function","function":{"name":"create_team_memory_candidate","arguments":"{\"category\":\"policy\",\"content\":\"Deployments require peer review.\"}"}}]}}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"I created an unconfirmed team knowledge candidate."}}]}`)
	}))
	defer model.Close()
	t.Setenv("AI_PROVIDER", "openai_compatible")
	t.Setenv("AI_BASE_URL", model.URL)
	t.Setenv("AI_API_KEY", "test-only-key")
	t.Setenv("AI_MODEL", "test-model")
	deniedRequest := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(`{"instruction":"Add a deployment policy","confirmTeam":true,"allowTeamKnowledgeCandidate":true}`))
	deniedResponse := httptest.NewRecorder()
	deniedErr := server.executeAgent(deniedResponse, deniedRequest, User{ID: memberID, Role: "member"}, conversationID, sql.NullString{String: teamID, Valid: true})
	var deniedFailure failure
	if !errors.As(deniedErr, &deniedFailure) || deniedFailure.status != http.StatusForbidden || atomic.LoadInt32(&modelCalls) != 0 {
		t.Fatalf("member request must fail before model/tool execution: err=%v status=%d modelCalls=%d", deniedErr, deniedResponse.Code, atomic.LoadInt32(&modelCalls))
	}
	request := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(`{"instruction":"Add this deployment policy to team knowledge","confirmTeam":true,"allowTeamKnowledgeCandidate":true}`))
	response := httptest.NewRecorder()
	err = server.executeAgent(response, request, User{ID: actorID, Role: "team_admin"}, conversationID, sql.NullString{String: teamID, Valid: true})
	if err != nil {
		t.Fatalf("execute authorized team knowledge candidate: %v (%d %s)", err, response.Code, response.Body.String())
	}
	if response.Code != http.StatusOK || atomic.LoadInt32(&modelCalls) != 2 {
		t.Fatalf("response status=%d model calls=%d body=%s", response.Code, atomic.LoadInt32(&modelCalls), response.Body.String())
	}
	var memoryID, sourceMessageID string
	var confirmed sql.NullTime
	if err = server.DB.QueryRow(`SELECT HEX(id),HEX(source_message_id),confirmed_at FROM team_memories WHERE team_id=UNHEX(?) AND content='Deployments require peer review.'`, teamID).Scan(&memoryID, &sourceMessageID, &confirmed); err != nil {
		t.Fatal(err)
	}
	var sourceConversationID, sourceRole, sourceContent string
	if err = server.DB.QueryRow(`SELECT HEX(conversation_id),role,content FROM conversation_messages WHERE id=UNHEX(?)`, sourceMessageID).Scan(&sourceConversationID, &sourceRole, &sourceContent); err != nil {
		t.Fatal(err)
	}
	if confirmed.Valid || !strings.EqualFold(sourceConversationID, conversationID) || sourceRole != "user" || sourceContent != "Add this deployment policy to team knowledge" {
		t.Fatalf("candidate confirmation/source incorrect: confirmed=%v source=%s/%s/%q", confirmed.Valid, sourceConversationID, sourceRole, sourceContent)
	}
	teamContext, err := server.aiTeamContext(context.Background(), teamID, actorID)
	if err != nil {
		t.Fatal(err)
	}
	encodedContext, _ := json.Marshal(teamContext)
	if strings.Contains(string(encodedContext), "Deployments require peer review.") {
		t.Fatalf("unconfirmed candidate entered AI team context: %s", encodedContext)
	}
	var reviewerNotifications, candidateAudit, toolCalls int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE team_id=UNHEX(?) AND recipient_user_id=UNHEX(?) AND resource_type='team_memory' AND resource_id=UNHEX(?)`, teamID, reviewerID, memoryID).Scan(&reviewerNotifications); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE team_id=UNHEX(?) AND resource_id=UNHEX(?) AND action='agent.team_memory.candidate.create'`, teamID, memoryID).Scan(&candidateAudit); err != nil {
		t.Fatal(err)
	}
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM agent_tool_calls WHERE team_id=UNHEX(?) AND tool_name='create_team_memory_candidate' AND approval_status='pending' AND status='succeeded'`, teamID).Scan(&toolCalls); err != nil {
		t.Fatal(err)
	}
	if reviewerNotifications != 1 || candidateAudit != 1 || toolCalls != 1 {
		t.Fatalf("candidate transaction evidence missing: notifications=%d audit=%d toolCalls=%d", reviewerNotifications, candidateAudit, toolCalls)
	}
	creatorResponse := httptest.NewRecorder()
	if err = server.teamMemories(creatorResponse, httptest.NewRequest(http.MethodGet, "/", nil), User{ID: actorID}, teamID, "team_admin", []string{"teams", teamID, "memories"}); err != nil {
		t.Fatal(err)
	}
	otherResponse := httptest.NewRecorder()
	if err = server.teamMemories(otherResponse, httptest.NewRequest(http.MethodGet, "/", nil), User{ID: reviewerID}, teamID, "team_owner", []string{"teams", teamID, "memories"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(creatorResponse.Body.String(), "sourceConversationId") || strings.Contains(otherResponse.Body.String(), "sourceConversationId") || strings.Contains(otherResponse.Body.String(), "sourceExcerpt") {
		t.Fatalf("source conversation privacy mismatch: creator=%s reviewer=%s", creatorResponse.Body.String(), otherResponse.Body.String())
	}
	memberResponse := httptest.NewRecorder()
	if err = server.teamMemories(memberResponse, httptest.NewRequest(http.MethodGet, "/", nil), User{ID: memberID}, teamID, "member", []string{"teams", teamID, "memories"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(memberResponse.Body.String(), memoryID) || strings.Contains(memberResponse.Body.String(), "Deployments require peer review.") {
		t.Fatalf("unconfirmed candidate was visible to a regular member: %s", memberResponse.Body.String())
	}
	for _, tc := range []struct {
		name, arguments string
		wantStatus      int
	}{
		{name: "duplicate candidate", arguments: `{"category":"policy","content":"Deployments require peer review."}`, wantStatus: http.StatusConflict},
		{name: "sensitive candidate", arguments: `{"category":"policy","content":"Store the API key in the team wiki."}`, wantStatus: http.StatusBadRequest},
		{name: "missing one-request consent", arguments: `{"category":"policy","content":"A candidate requires explicit request consent."}`, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			call := agentFunctionCall{ID: "call-rejected", Type: "function"}
			call.Function.Name = "create_team_memory_candidate"
			call.Function.Arguments = tc.arguments
			consent := []bool{true}
			if tc.name == "missing one-request consent" {
				consent = nil
			}
			_, _, _, toolErr := server.runSelectedAgentTool(context.Background(), User{ID: actorID}, teamID, conversationID, call, "test", "test-model", false, false, false, nil, consent...)
			var apiErr failure
			if !errors.As(toolErr, &apiErr) || apiErr.status != tc.wantStatus {
				t.Fatalf("expected status %d, got %v", tc.wantStatus, toolErr)
			}
		})
	}
	memberCall := agentFunctionCall{ID: "call-member", Type: "function"}
	memberCall.Function.Name = "create_team_memory_candidate"
	memberCall.Function.Arguments = `{"category":"policy","content":"A member cannot bypass candidate permissions."}`
	_, _, _, memberErr := server.runSelectedAgentTool(context.Background(), User{ID: memberID}, teamID, conversationID, memberCall, "test", "test-model", false, false, false, nil, true)
	var memberAPIError failure
	if !errors.As(memberErr, &memberAPIError) || memberAPIError.status != http.StatusForbidden {
		t.Fatalf("member candidate write must be denied, got %v", memberErr)
	}
	var concurrentWins, concurrentDuplicates int32
	var concurrentErrors sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		concurrentErrors.Add(1)
		go func(candidateID string) {
			defer concurrentErrors.Done()
			tx, txErr := server.DB.BeginTx(context.Background(), nil)
			if txErr != nil {
				errCh <- txErr
				return
			}
			txErr = server.createAgentTeamMemoryCandidateTx(context.Background(), tx, actorID, teamID, conversationID, sourceMessageID, candidateID, agentTeamMemoryArgs{Category: "policy", Content: "Concurrent candidates must serialize."}, "test", "test-model")
			if txErr != nil {
				_ = tx.Rollback()
				var conflict failure
				if errors.As(txErr, &conflict) && conflict.status == http.StatusConflict {
					atomic.AddInt32(&concurrentDuplicates, 1)
					return
				}
				errCh <- txErr
				return
			}
			if txErr = tx.Commit(); txErr != nil {
				errCh <- txErr
				return
			}
			atomic.AddInt32(&concurrentWins, 1)
		}(id())
	}
	concurrentErrors.Wait()
	close(errCh)
	for concurrentErr := range errCh {
		t.Errorf("concurrent candidate creation failed unexpectedly: %v", concurrentErr)
	}
	if atomic.LoadInt32(&concurrentWins) != 1 || atomic.LoadInt32(&concurrentDuplicates) != 1 {
		t.Fatalf("concurrent duplicate serialization mismatch: wins=%d conflicts=%d", atomic.LoadInt32(&concurrentWins), atomic.LoadInt32(&concurrentDuplicates))
	}
}

func TestModelSelectedSandboxToolUsesTemporaryInputsAndOutputs(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	if !agentSandboxAvailable() {
		t.Skip("the isolated Python sandbox is unavailable")
	}
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	userID, conversationID := id(), id()
	workspaceKey := make([]byte, 32)
	if _, err = rand.Read(workspaceKey); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_WORKSPACE_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(workspaceKey))
	t.Cleanup(func() {
		server.DB.Exec(`DELETE FROM agent_workspace_files WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM agent_tool_calls WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM agent_runs WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, conversationID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		server.DB.Close()
	})
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "agent-sandbox-"+userID+"@example.invalid", "Agent Sandbox Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),'personal','Agent sandbox test')`, conversationID, userID); err != nil {
		t.Fatal(err)
	}
	var calls int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
		}
		count := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			if len(request.Tools) != 1 || request.Tools[0].Function.Name != "run_python" {
				t.Errorf("personal sandbox request received unexpected tools: %+v", request.Tools)
			}
			arguments, _ := json.Marshal(map[string]string{"code": "value = open('input.txt').read()\nprint(value.upper())\nopen('generated.txt','w').write(value[::-1])\n"})
			response, _ := json.Marshal(map[string]any{"model": "test-model", "choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{"id": "call-sandbox", "type": "function", "function": map[string]string{"name": "run_python", "arguments": string(arguments)}}}}}}})
			_, _ = w.Write(response)
			return
		}
		_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"The temporary file transformation is complete."}}]}`)
	}))
	defer model.Close()
	t.Setenv("AI_PROVIDER", "openai_compatible")
	t.Setenv("AI_BASE_URL", model.URL)
	t.Setenv("AI_API_KEY", "test-only-key")
	t.Setenv("AI_MODEL", "test-model")
	input := map[string]any{
		"instruction":        "Transform the uploaded text file",
		"allowCodeExecution": true,
		"files":              []sandboxInputFile{{Name: "input.txt", Content: "Temporary private input"}},
	}
	body, _ := json.Marshal(input)
	request := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(string(body)))
	response := httptest.NewRecorder()
	if err = server.executeAgent(response, request, User{ID: userID}, conversationID, sql.NullString{}); err != nil {
		t.Fatalf("execute temporary sandbox tool: %v (%d %s)", err, response.Code, response.Body.String())
	}
	var result struct {
		Data struct {
			Tool   string `json:"tool"`
			Result struct {
				Stdout string            `json:"stdout"`
				Files  map[string]string `json:"files"`
			} `json:"result"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Tool != "run_python" || !strings.Contains(result.Data.Result.Stdout, "TEMPORARY PRIVATE INPUT") || result.Data.Result.Files["generated.txt"] != "tupni etavirp yraropmeT" || atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("unexpected temporary tool result: %+v calls=%d", result.Data, atomic.LoadInt32(&calls))
	}
	var summary string
	if err = server.DB.QueryRow(`SELECT CONCAT(CAST(c.argument_summary AS CHAR),CAST(c.result_summary AS CHAR)) FROM agent_tool_calls c WHERE c.user_id=UNHEX(?) AND c.tool_name='run_python'`, userID).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(summary, "Temporary private input") || strings.Contains(summary, "value = open") || !strings.Contains(summary, "generated.txt") {
		t.Fatalf("tool audit exposed input/code or omitted output names: %s", summary)
	}
	var messages int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM conversation_messages WHERE conversation_id=UNHEX(?)`, conversationID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages != 2 {
		t.Fatalf("conversation persistence count=%d; want 2", messages)
	}
	var persistedFiles int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM agent_workspace_files WHERE user_id=UNHEX(?)`, userID).Scan(&persistedFiles); err != nil {
		t.Fatal(err)
	}
	if persistedFiles != 0 {
		t.Fatalf("temporary sandbox output unexpectedly persisted %d files", persistedFiles)
	}

	// Persistent output is a separate, explicit consent path. Execute the same
	// sandbox transformation again and prove only this run writes encrypted
	// output into the account's MySQL workspace.
	atomic.StoreInt32(&calls, 0)
	input["persistWorkspaceFiles"] = true
	body, _ = json.Marshal(input)
	request = httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(string(body)))
	response = httptest.NewRecorder()
	if err = server.executeAgent(response, request, User{ID: userID}, conversationID, sql.NullString{}); err != nil {
		t.Fatalf("execute explicitly persistent sandbox tool: %v (%d %s)", err, response.Code, response.Body.String())
	}
	var fileID string
	var encrypted []byte
	if err = server.DB.QueryRow(`SELECT HEX(id),encrypted_content FROM agent_workspace_files WHERE user_id=UNHEX(?) AND name='generated.txt'`, userID).Scan(&fileID, &encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("tupni etavirp yraropmeT")) {
		t.Fatal("persistent sandbox output was stored in plaintext")
	}
	content, err := decryptAgentWorkspaceFile(userID, fileID, "generated.txt", encrypted)
	if err != nil || content != "tupni etavirp yraropmeT" {
		t.Fatalf("persisted sandbox output mismatch: content=%q error=%v", content, err)
	}
}

func TestAgentToolSelectionAdvertisesShellOnlyWithPerCallConsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Parallel bool `json:"parallel_tool_calls"`
			Tools    []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Parallel || len(request.Tools) != 1 || request.Tools[0].Function.Name != "run_shell" {
			t.Errorf("shell was offered without a dedicated user grant: %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"tool_calls":[{"id":"shell-call","type":"function","function":{"name":"run_shell","arguments":"{\"command\":\"printf safe\"}"}}]}}]}`)
	}))
	defer server.Close()
	t.Setenv("AI_BASE_URL", server.URL)
	t.Setenv("AI_API_KEY", "test-only-key")
	t.Setenv("AI_MODEL", "test-model")
	model, _, calls, err := requestAgentToolChoiceWithShell(context.Background(), []aiMessage{{Role: "user", Content: "run a command"}}, false, false, true)
	if err != nil || model != "test-model" || len(calls) != 1 || calls[0].Function.Name != "run_shell" {
		t.Fatalf("model=%s calls=%+v err=%v", model, calls, err)
	}
}

func TestShellToolRequiresExplicitPerCallConsent(t *testing.T) {
	server := &Server{}
	call := agentFunctionCall{ID: "shell-call", Type: "function", Function: struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{Name: "run_shell", Arguments: `{"command":"echo no"}`}}
	_, _, _, err := server.runSelectedAgentTool(context.Background(), User{}, "", "", call, "test", "test", false, false, false, nil)
	if err == nil || err.Error() != "AGENT_SHELL_CONSENT_REQUIRED" {
		t.Fatalf("shell executed without explicit consent, err=%v", err)
	}
}

func TestModelSelectedShellToolUsesAuthorizedSandboxAndEncryptedWorkspace(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	if !agentShellAvailable() {
		t.Skip("the isolated Bash sandbox is unavailable")
	}
	server, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	userID, conversationID := id(), id()
	workspaceKey := make([]byte, 32)
	if _, err = rand.Read(workspaceKey); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_WORKSPACE_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(workspaceKey))
	t.Cleanup(func() {
		server.DB.Exec(`DELETE FROM agent_workspace_files WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM agent_tool_calls WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM agent_runs WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, conversationID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		server.DB.Close()
	})
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "agent-shell-"+userID+"@example.invalid", "Agent Shell Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),'personal','Agent shell test')`, conversationID, userID); err != nil {
		t.Fatal(err)
	}
	var calls int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
		}
		count := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			if len(request.Tools) != 1 || request.Tools[0].Function.Name != "run_shell" {
				t.Errorf("model was offered shell without the dedicated authorization: %+v", request.Tools)
			}
			arguments, _ := json.Marshal(map[string]string{"command": "cat input.txt > generated.txt; cat generated.txt"})
			response, _ := json.Marshal(map[string]any{"model": "test-model", "choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{map[string]any{"id": "shell-call", "type": "function", "function": map[string]string{"name": "run_shell", "arguments": string(arguments)}}}}}}})
			_, _ = w.Write(response)
			return
		}
		_, _ = fmt.Fprint(w, `{"model":"test-model","choices":[{"message":{"content":"The isolated shell transformation is complete."}}]}`)
	}))
	defer model.Close()
	t.Setenv("AI_PROVIDER", "openai_compatible")
	t.Setenv("AI_BASE_URL", model.URL)
	t.Setenv("AI_API_KEY", "test-only-key")
	t.Setenv("AI_MODEL", "test-model")
	input := map[string]any{
		"instruction":         "Transform the selected text with a shell command",
		"allowShellExecution": true,
		"files":               []sandboxInputFile{{Name: "input.txt", Content: "Selected shell input"}},
	}
	run := func() map[string]any {
		body, _ := json.Marshal(input)
		request := httptest.NewRequest(http.MethodPost, "/agent", strings.NewReader(string(body)))
		response := httptest.NewRecorder()
		if runErr := server.executeAgent(response, request, User{ID: userID}, conversationID, sql.NullString{}); runErr != nil {
			t.Fatalf("execute model-selected shell tool: %v (%d %s)", runErr, response.Code, response.Body.String())
		}
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if decodeErr := json.Unmarshal(response.Body.Bytes(), &envelope); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return envelope.Data
	}
	first := run()
	result, _ := first["result"].(map[string]any)
	files, _ := result["files"].(map[string]any)
	if first["tool"] != "run_shell" || files["generated.txt"] != "Selected shell input" || result["persisted"] != false {
		t.Fatalf("unexpected temporary shell result: %+v", first)
	}
	var persistedFiles int
	if err = server.DB.QueryRow(`SELECT COUNT(*) FROM agent_workspace_files WHERE user_id=UNHEX(?)`, userID).Scan(&persistedFiles); err != nil || persistedFiles != 0 {
		t.Fatalf("temporary shell output persisted: count=%d err=%v", persistedFiles, err)
	}

	atomic.StoreInt32(&calls, 0)
	input["persistWorkspaceFiles"] = true
	second := run()
	var fileID string
	var encrypted []byte
	if err = server.DB.QueryRow(`SELECT HEX(id),encrypted_content FROM agent_workspace_files WHERE user_id=UNHEX(?) AND name='generated.txt'`, userID).Scan(&fileID, &encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("Selected shell input")) {
		t.Fatal("persistent shell output was stored in plaintext")
	}
	content, err := decryptAgentWorkspaceFile(userID, fileID, "generated.txt", encrypted)
	if err != nil || content != "Selected shell input" {
		t.Fatalf("persisted shell output mismatch: %q err=%v result=%+v", content, err, second)
	}
	var summary string
	if err = server.DB.QueryRow(`SELECT CONCAT(CAST(argument_summary AS CHAR),CAST(result_summary AS CHAR)) FROM agent_tool_calls WHERE user_id=UNHEX(?) AND tool_name='run_shell' ORDER BY id DESC LIMIT 1`, userID).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(summary, "Selected shell input") || strings.Contains(summary, "cat input.txt") {
		t.Fatalf("shell audit exposed content or command: %s", summary)
	}
}
