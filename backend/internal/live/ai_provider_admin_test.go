package live

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The client requires the standard JSON envelope after every successful
// mutation. A bare 200 makes the saved change appear to have failed.
func TestAIProviderPatchReturnsEnvelopeAndPersists(t *testing.T) {
	s, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	defer s.DB.Close()
	if _, err = aiConfigKey(); err != nil {
		t.Skipf("AI encryption key is required: %v", err)
	}
	userID, providerID := id(), id()
	defer func() {
		_, _ = s.DB.Exec(`DELETE FROM ai_providers WHERE id=UNHEX(?)`, providerID)
		_, _ = s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
	}()
	if _, err = s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, fmt.Sprintf("ai-admin-%s@example.invalid", userID), "AI Admin Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	key, err := encryptAIKey(providerID, "test-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key_ciphertext,models,created_by) VALUES(UNHEX(?),'Primary','https://example.invalid/v1',?,'["model-a"]',UNHEX(?))`, providerID, key, userID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/platform/ai/providers/"+providerID, strings.NewReader(`{"enabled":false,"priority":2}`))
	response := httptest.NewRecorder()
	if err = s.aiAdmin(response, request, User{ID: userID, SuperAdmin: true}, "platform/ai/providers/"+providerID); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}
	var envelope struct {
		Code string `json:"code"`
		Data struct {
			Updated bool `json:"updated"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Code != "OK" || !envelope.Data.Updated {
		t.Fatalf("invalid API envelope: %s (%v)", response.Body.String(), err)
	}
	var enabled bool
	var priority int
	if err = s.DB.QueryRow(`SELECT enabled,priority FROM ai_providers WHERE id=UNHEX(?)`, providerID).Scan(&enabled, &priority); err != nil || enabled || priority != 2 {
		t.Fatalf("provider change was not persisted: enabled=%v priority=%d err=%v", enabled, priority, err)
	}
}

func TestStreamAIProviderRejectsEmptyCompletedStream(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	_, wrote, err := streamAIProvider(context.Background(), aiProviderConfig{BaseURL: provider.URL, APIKey: "test-secret", Model: "test-model"}, []aiMessage{{Role: "user", Content: "hello"}}, func(string) error { return nil })
	if err == nil || wrote {
		t.Fatalf("empty completed stream must be rejected before failover: wrote=%v err=%v", wrote, err)
	}
}

func TestAIProviderFailoverAndCooldown(t *testing.T) {
	s, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	defer s.DB.Close()
	if _, err = aiConfigKey(); err != nil {
		t.Skipf("AI encryption key is required: %v", err)
	}
	var failedCalls, healthyCalls atomic.Int32
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		failedCalls.Add(1)
		http.Error(w, "provider unavailable", http.StatusBadGateway)
	}))
	defer failing.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		healthyCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"healthy reply"}}]}`)
	}))
	defer healthy.Close()
	userID, failingID, healthyID := id(), id(), id()
	defer func() {
		_, _ = s.DB.Exec(`DELETE FROM ai_providers WHERE id IN (UNHEX(?),UNHEX(?))`, failingID, healthyID)
		_, _ = s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
	}()
	if _, err = s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, fmt.Sprintf("ai-failover-%s@example.invalid", userID), "AI Failover Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []struct {
		id, name, url, models string
		priority              int
	}{
		{failingID, "Failing", failing.URL, `["broken-a","broken-b"]`, 1},
		{healthyID, "Healthy", healthy.URL, `["working"]`, 2},
	} {
		key, keyErr := encryptAIKey(provider.id, "test-secret-key")
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		if _, err = s.DB.Exec(`INSERT INTO ai_providers(id,name,base_url,api_key_ciphertext,models,priority,created_by) VALUES(UNHEX(?),?,?,?,?,?,UNHEX(?))`, provider.id, provider.name, provider.url, key, provider.models, provider.priority, userID); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		var output strings.Builder
		model, replyErr := s.streamAIReply(context.Background(), []aiMessage{{Role: "user", Content: "hello"}}, func(part string) error { _, e := output.WriteString(part); return e })
		if replyErr != nil || model != "working" || output.String() != "healthy reply" {
			t.Fatalf("failover reply=%q model=%q error=%v", output.String(), model, replyErr)
		}
	}
	if failedCalls.Load() != 4 || healthyCalls.Load() != 2 {
		t.Fatalf("expected first provider to be tried before fallback: failed=%d healthy=%d", failedCalls.Load(), healthyCalls.Load())
	}
	var failures int
	var cooldown bool
	if err = s.DB.QueryRow(`SELECT failure_count,cooldown_until IS NOT NULL FROM ai_providers WHERE id=UNHEX(?)`, failingID).Scan(&failures, &cooldown); err != nil || failures < aiFailureThreshold || !cooldown {
		t.Fatalf("circuit breaker state failures=%d cooldown=%v error=%v", failures, cooldown, err)
	}
	var output strings.Builder
	model, err := s.streamAIReply(context.Background(), []aiMessage{{Role: "user", Content: "hello"}}, func(part string) error { _, e := output.WriteString(part); return e })
	if err != nil || model != "working" || failedCalls.Load() != 4 || healthyCalls.Load() != 3 {
		t.Fatalf("cooldown failed: model=%q failed=%d healthy=%d error=%v", model, failedCalls.Load(), healthyCalls.Load(), err)
	}
}
