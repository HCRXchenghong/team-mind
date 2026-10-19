package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamAIReplyReadsProviderChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("provider authorization header missing")
		}
		var request struct {
			Stream   bool        `json:"stream"`
			Messages []aiMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !request.Stream || len(request.Messages) != 3 || request.Messages[1].Content != "prior turn" || request.Messages[2].Content != "current turn" {
			t.Errorf("conversation context was not streamed: %+v", request)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n")
		w.(http.Flusher).Flush()
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	t.Setenv("AI_BASE_URL", server.URL+"/v1")
	t.Setenv("AI_API_KEY", "test-secret")
	t.Setenv("AI_MODEL", "test-model")
	var output strings.Builder
	model, err := streamAIReply(context.Background(), "openai_compatible", []aiMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "prior turn"},
		{Role: "user", Content: "current turn"},
	}, func(part string) error {
		output.WriteString(part)
		return nil
	})
	if err != nil || model != "test-model" || output.String() != "hello world" {
		t.Fatalf("stream result model=%q output=%q error=%v", model, output.String(), err)
	}
}

func TestStreamAIReplyRejectsTruncatedProviderStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer server.Close()
	t.Setenv("AI_BASE_URL", server.URL)
	t.Setenv("AI_API_KEY", "test-secret")
	t.Setenv("AI_MODEL", "test-model")
	_, err := streamAIReply(context.Background(), "openai_compatible", []aiMessage{{Role: "user", Content: "hello"}}, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "AI_FAILED") {
		t.Fatalf("expected truncated stream error, got %v", err)
	}
}

func TestStreamAIReplyAcceptsNonStreamingCompatibleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"complete"}}]}`)
	}))
	defer server.Close()
	t.Setenv("AI_BASE_URL", server.URL)
	t.Setenv("AI_API_KEY", "test-secret")
	t.Setenv("AI_MODEL", "test-model")
	var output strings.Builder
	_, err := streamAIReply(context.Background(), "openai_compatible", []aiMessage{{Role: "user", Content: "hello"}}, func(part string) error { output.WriteString(part); return nil })
	if err != nil || output.String() != "complete" {
		t.Fatalf("non-stream response output=%q error=%v", output.String(), err)
	}
}

func TestParseExtractedMemoryResponseRequiresValidUserSource(t *testing.T) {
	transcript := []memoryTranscriptMessage{{ID: "user-id", Role: "user", Content: "I prefer morning meetings"}, {ID: "assistant-id", Role: "assistant", Content: "You said mornings work"}}
	valid := `{"candidates":[{"category":"profile","content":"Prefers morning meetings","sourceMessageIndex":0,"confidence":0.93}]}`
	candidates, err := parseExtractedMemoryResponse(valid, transcript)
	if err != nil || len(candidates) != 1 || candidates[0].SourceMessageIndex != 0 {
		t.Fatalf("valid extraction candidates=%+v error=%v", candidates, err)
	}
	for _, invalid := range []string{
		`{"candidates":[{"category":"profile","content":"Assistant claim","sourceMessageIndex":1,"confidence":0.9}]}`,
		`{"candidates":[{"category":"profile","content":"password is abc","sourceMessageIndex":0,"confidence":0.9}]}`,
		`{"candidates":[{"category":"profile","content":"Morning","sourceMessageIndex":0,"confidence":1.2}]}`,
		"```json\n" + valid + "\n```",
	} {
		if _, err = parseExtractedMemoryResponse(invalid, transcript); err == nil {
			t.Errorf("expected invalid provider output to be rejected: %s", invalid)
		}
	}
}

func TestExtractConversationMemoriesStoresReviewableCandidates(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	defer server.DB.Close()
	userID, conversationID, messageID := id(), id(), id()
	defer func() {
		server.DB.Exec(`DELETE FROM user_memories WHERE owner_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM agent_runs WHERE user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id=UNHEX(?)`, userID)
		server.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, conversationID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
	}()
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "ai-extract-"+userID+"@example.invalid", "AI Extract", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,visibility) VALUES(UNHEX(?),UNHEX(?),'personal')`, conversationID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversation_messages(id,conversation_id,role,content) VALUES(UNHEX(?),UNHEX(?),'user','I prefer written project updates')`, messageID, conversationID); err != nil {
		t.Fatal(err)
	}
	providerCalls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls++
		var request struct {
			Messages []aiMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode provider request: %v", err)
		}
		if len(request.Messages) != 2 || !strings.Contains(request.Messages[1].Content, "I prefer written project updates") {
			t.Errorf("unexpected extraction context: %+v", request.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"candidates\":[{\"category\":\"profile\",\"content\":\"Prefers written project updates\",\"sourceMessageIndex\":0,\"confidence\":0.95}]}"}}]}`)
	}))
	defer provider.Close()
	t.Setenv("AI_PROVIDER", "openai_compatible")
	t.Setenv("AI_BASE_URL", provider.URL)
	t.Setenv("AI_MODEL", "test-model")
	t.Setenv("AI_API_KEY", "test-only-secret")
	invoke := func() memoryExtractionTestResult {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"confirmTeam":false}`))
		recorder := httptest.NewRecorder()
		if err := server.extractConversationMemories(recorder, request, User{ID: userID}, conversationID, sql.NullString{}); err != nil {
			t.Fatalf("extract memories: %v", err)
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var envelope struct {
			Data memoryExtractionTestResult `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	first := invoke()
	if len(first.Created) != 1 || first.Duplicates != 0 || first.Simulated {
		t.Fatalf("unexpected first extraction: %+v", first)
	}
	var confirmed sql.NullTime
	var source string
	if err = server.DB.QueryRow(`SELECT HEX(source_message_id),confirmed_at FROM user_memories WHERE id=UNHEX(?) AND owner_user_id=UNHEX(?)`, first.Created[0].ID, userID).Scan(&source, &confirmed); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(source) != messageID || confirmed.Valid {
		t.Fatalf("candidate source=%s confirmed=%v", source, confirmed.Valid)
	}
	second := invoke()
	if len(second.Created) != 0 || second.Duplicates != 1 || providerCalls != 2 {
		t.Fatalf("duplicate extraction result=%+v provider calls=%d", second, providerCalls)
	}
}

func TestConversationDeletionPurgesMessagesAndDetachesConfirmedMemorySources(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	defer server.DB.Close()
	ownerID, otherID, conversationID := id(), id(), id()
	messageID, confirmedMemoryID, pendingMemoryID := id(), id(), id()
	accessToken := token()
	defer func() {
		server.DB.Exec(`DELETE FROM agent_tool_calls WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM agent_runs WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM user_memories WHERE owner_user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM conversations WHERE id=UNHEX(?)`, conversationID)
		server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
		server.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?))`, ownerID, otherID)
	}()
	for _, item := range []struct{ id, email string }{{ownerID, "conversation-owner-" + ownerID + "@example.invalid"}, {otherID, "conversation-other-" + otherID + "@example.invalid"}} {
		if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, item.id, item.email, "Conversation Test", "not-a-login"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 1 HOUR))`, id(), ownerID, hash(accessToken)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversations(id,owner_user_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),'personal','private conversation')`, conversationID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO conversation_messages(id,conversation_id,role,content) VALUES(UNHEX(?),UNHEX(?),'user','private message')`, messageID, conversationID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO user_memories(id,owner_user_id,category,content,source_message_id,confirmed_at) VALUES(UNHEX(?),UNHEX(?),'profile','confirmed memory',UNHEX(?),UTC_TIMESTAMP(3))`, confirmedMemoryID, ownerID, messageID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO user_memories(id,owner_user_id,category,content,source_message_id) VALUES(UNHEX(?),UNHEX(?),'profile','pending candidate',UNHEX(?))`, pendingMemoryID, ownerID, messageID); err != nil {
		t.Fatal(err)
	}
	runID := id()
	if _, err = server.DB.Exec(`INSERT INTO agent_runs(id,user_id,conversation_id,provider,status,context_summary) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'test','succeeded','{}')`, runID, ownerID, conversationID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO agent_tool_calls(id,run_id,user_id,tool_name,argument_summary,approval_status,status) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'list_team_tasks','{}','not_required','succeeded')`, id(), runID, ownerID); err != nil {
		t.Fatal(err)
	}
	invoke := func(userID, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/me/conversations/"+conversationID, nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		return recorder
	}
	otherToken := token()
	if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 1 HOUR))`, id(), otherID, hash(otherToken)); err != nil {
		t.Fatal(err)
	}
	if denied := invoke(otherID, otherToken); denied.Code != http.StatusNotFound {
		t.Fatalf("non-owner delete status=%d body=%s", denied.Code, denied.Body.String())
	}
	deleted := invoke(ownerID, accessToken)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"messagesPurged":true`) {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	var conversationCount, messageCount, pendingCount, confirmedCount, agentRuns, toolCalls, detachedSource int
	checks := []struct {
		query string
		args  []any
		dest  *int
	}{
		{`SELECT COUNT(*) FROM conversations WHERE id=UNHEX(?)`, []any{conversationID}, &conversationCount},
		{`SELECT COUNT(*) FROM conversation_messages WHERE id=UNHEX(?)`, []any{messageID}, &messageCount},
		{`SELECT COUNT(*) FROM user_memories WHERE id=UNHEX(?)`, []any{pendingMemoryID}, &pendingCount},
		{`SELECT COUNT(*) FROM user_memories WHERE id=UNHEX(?)`, []any{confirmedMemoryID}, &confirmedCount},
		{`SELECT COUNT(*) FROM agent_runs WHERE id=UNHEX(?)`, []any{runID}, &agentRuns},
		{`SELECT COUNT(*) FROM agent_tool_calls WHERE run_id=UNHEX(?)`, []any{runID}, &toolCalls},
		{`SELECT COUNT(*) FROM user_memories WHERE id=UNHEX(?) AND source_message_id IS NULL`, []any{confirmedMemoryID}, &detachedSource},
	}
	for _, check := range checks {
		if err = server.DB.QueryRow(check.query, check.args...).Scan(check.dest); err != nil {
			t.Fatal(err)
		}
	}
	if conversationCount+messageCount+pendingCount+agentRuns+toolCalls != 0 || confirmedCount != 1 || detachedSource != 1 {
		t.Fatalf("unexpected conversation cleanup: conversation=%d message=%d pending=%d confirmed=%d sourceDetached=%d runs=%d calls=%d", conversationCount, messageCount, pendingCount, confirmedCount, detachedSource, agentRuns, toolCalls)
	}
}

type memoryExtractionTestResult struct {
	Created []struct {
		ID string `json:"id"`
	} `json:"created"`
	Duplicates int  `json:"duplicates"`
	Simulated  bool `json:"simulated"`
}
