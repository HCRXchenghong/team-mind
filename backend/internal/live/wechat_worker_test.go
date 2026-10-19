package live

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWechatProtocolMessageIDAndText(t *testing.T) {
	var response pluginWechatUpdates
	d := json.NewDecoder(bytes.NewBufferString(`{"ret":0,"msgs":[{"message_id":18446744073709551615,"from_user_id":"user","message_type":1,"item_list":[{"type":1,"text_item":{"text":"hello"}},{"type":2}]}]}`))
	d.UseNumber()
	if err := d.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if got := response.Messages[0].MessageID.String(); got != "18446744073709551615" {
		t.Fatalf("message ID lost precision: %s", got)
	}
	if got := inboundText(response.Messages[0]); got != "hello" {
		t.Fatalf("unexpected text %q", got)
	}
}

func TestConfiguredWechatAIProviderRejectsMockOrIncompleteConfiguration(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		baseURL  string
		model    string
		apiKey   string
		want     string
	}{
		{name: "unset provider defaults to no real provider"},
		{name: "mock replies must not go to WeChat", provider: "mock"},
		{name: "missing API key", provider: "openai_compatible", baseURL: "https://ai.example/v1", model: "model"},
		{name: "unknown provider", provider: "other", baseURL: "https://ai.example/v1", model: "model", apiKey: "secret"},
		{name: "configured real provider", provider: "openai_compatible", baseURL: "https://ai.example/v1", model: "model", apiKey: "secret", want: "openai_compatible"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AI_PROVIDER", tc.provider)
			t.Setenv("AI_BASE_URL", tc.baseURL)
			t.Setenv("AI_MODEL", tc.model)
			t.Setenv("AI_API_KEY", tc.apiKey)
			got, err := configuredWechatAIProvider()
			if tc.want == "" {
				if err == nil || got != "" {
					t.Fatalf("expected configuration rejection; provider=%q error=%v", got, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("provider=%q error=%v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestWechatWorkerRejectsMockAndDeliversConfiguredProviderReply(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("AI_PROVIDER", "mock")
	t.Setenv("AI_BASE_URL", "")
	t.Setenv("AI_MODEL", "")
	t.Setenv("AI_API_KEY", "")
	s, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := s.DB.Close(); closeErr != nil {
			t.Errorf("close integration-test database: %v", closeErr)
		}
	})

	userID, teamID, accountID, conversationID, messageID := id(), id(), id(), id(), id()
	messageKey := "mock-reply-" + messageID
	senderID := "bound-wechat-sender-" + accountID
	cleanup := func() {
		statements := []struct {
			query string
			args  []any
		}{
			{`DELETE FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?)`, []any{accountID}},
			{`DELETE FROM channel_sync_states WHERE channel_account_id=UNHEX(?)`, []any{accountID}},
			{`DELETE FROM channel_conversations WHERE channel_account_id=UNHEX(?)`, []any{accountID}},
			{`DELETE FROM agent_tool_calls WHERE run_id IN (SELECT id FROM agent_runs WHERE conversation_id=UNHEX(?))`, []any{conversationID}},
			{`DELETE FROM agent_runs WHERE conversation_id=UNHEX(?)`, []any{conversationID}},
			{`DELETE FROM conversations WHERE id=UNHEX(?)`, []any{conversationID}},
			{`DELETE FROM channel_bindings WHERE channel_account_id=UNHEX(?)`, []any{accountID}},
			{`DELETE FROM channel_accounts WHERE id=UNHEX(?)`, []any{accountID}},
			{`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, []any{teamID}},
			{`DELETE FROM teams WHERE id=UNHEX(?)`, []any{teamID}},
			{`DELETE FROM users WHERE id=UNHEX(?)`, []any{userID}},
		}
		for _, statement := range statements {
			if _, cleanupErr := s.DB.Exec(statement.query, statement.args...); cleanupErr != nil {
				t.Errorf("clean integration-test fixture: %v", cleanupErr)
				return
			}
		}
	}
	t.Cleanup(cleanup)

	credentials, err := encryptWechat("channel-test-token")
	if err != nil {
		t.Fatal(err)
	}
	sender, err := encryptWechat(senderID)
	if err != nil {
		t.Fatal(err)
	}
	contextToken, err := encryptWechat("reply-context-token")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "wechat-mock-"+userID+"@example.invalid", "Worker Test", "not-a-login"); err == nil {
		_, err = tx.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Worker Test',?,'active',UNHEX(?),'{}')`, teamID, "worker-"+teamID, userID)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status) VALUES(UNHEX(?),UNHEX(?),'team_owner','active')`, teamID, userID)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO channel_accounts(id,provider,provider_user_hash,scanner_hash,provider_base_url,encrypted_credentials,status) VALUES(UNHEX(?),'openclaw-weixin',?,?, 'https://hk.weixin.qq.com',?,'active')`, accountID, hash("mock-bot-"+accountID), hash(senderID), credentials)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO channel_bindings(channel_account_id,user_id,current_team_id) VALUES(UNHEX(?),UNHEX(?),UNHEX(?))`, accountID, userID, teamID)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO conversations(id,owner_user_id,team_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'team_context','WeChat test')`, conversationID, userID, teamID)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO conversation_messages(id,conversation_id,role,content) VALUES(UNHEX(?),UNHEX(?),'user','hello')`, messageID, conversationID)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO channel_conversations(channel_account_id,sender_hash,user_id,team_id,conversation_id) VALUES(UNHEX(?),?,UNHEX(?),UNHEX(?),UNHEX(?))`, accountID, hash(senderID), userID, teamID, conversationID)
	}
	if err == nil {
		_, err = tx.Exec(`INSERT INTO channel_message_deliveries(channel_account_id,provider_message_id,sender_hash,user_id,team_id,conversation_id,inbound_message_id,sender_ciphertext,context_ciphertext) VALUES(UNHEX(?),?,?,UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,?)`, accountID, messageKey, hash(senderID), userID, teamID, conversationID, messageID, sender, contextToken)
	}
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var sendRequests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/teammind-wechat-bridge/v1/channel/send-text" || r.Method != http.MethodPost {
			t.Errorf("unexpected Tencent bridge request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
			t.Errorf("Tencent bridge request did not authenticate")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var request struct {
			Token string `json:"token"`
			To    string `json:"to"`
			Text  string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode Tencent bridge request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.Token != "channel-token" || request.To != senderID || request.Text != "reply from configured model" {
			t.Errorf("unexpected Tencent bridge payload: token-present=%t recipient-matches=%t text=%q", request.Token != "", request.To == senderID, request.Text)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		sendRequests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "messageId": "provider-reply-id"})
	}))
	defer upstream.Close()
	gateway := &openClawWechatGateway{baseURL: upstream.URL + "/teammind-wechat-bridge/v1", token: strings.Repeat("x", 32), client: upstream.Client()}
	err = s.deliverChannelReply(context.Background(), gateway, accountID, wechatCredentials{Token: "channel-token", BaseURL: "https://hk.weixin.qq.com"}, messageKey, userID, teamID, conversationID, messageID, sender, contextToken, nil, 0)
	if err == nil {
		t.Fatal("expected mock provider to be rejected for a real WeChat delivery")
	}
	if sendRequests != 0 {
		t.Fatalf("mock reply was sent to the WeChat bridge %d times", sendRequests)
	}
	var status string
	var attempts int
	if err = s.DB.QueryRow(`SELECT status,attempts FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?) AND provider_message_id=?`, accountID, messageKey).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempts != 1 {
		t.Fatalf("mock reply delivery should be retained as a retryable failure; status=%s attempts=%d", status, attempts)
	}

	var modelRequests int
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("unexpected model request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer local-provider-test-key" {
			t.Errorf("model request did not use the configured server-side API key")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var request struct {
			Model    string      `json:"model"`
			Messages []aiMessage `json:"messages"`
			Stream   bool        `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode model request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.Model != "local-provider-test-model" || !request.Stream {
			t.Errorf("model request did not preserve provider configuration: model=%q stream=%t", request.Model, request.Stream)
		}
		foundPrompt := false
		for _, message := range request.Messages {
			if message.Role == "user" && message.Content == "hello" {
				foundPrompt = true
			}
		}
		if !foundPrompt {
			t.Errorf("configured model request omitted the inbound user message")
			http.Error(w, "missing prompt", http.StatusBadRequest)
			return
		}
		modelRequests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "local-provider-test-model", "choices": []any{map[string]any{"message": map[string]string{"content": "reply from configured model"}}}})
	}))
	defer model.Close()
	t.Setenv("AI_PROVIDER", "openai_compatible")
	t.Setenv("AI_BASE_URL", model.URL)
	t.Setenv("AI_MODEL", "local-provider-test-model")
	t.Setenv("AI_API_KEY", "local-provider-test-key")
	if err = s.deliverChannelReply(context.Background(), gateway, accountID, wechatCredentials{Token: "channel-token", BaseURL: "https://hk.weixin.qq.com"}, messageKey, userID, teamID, conversationID, messageID, sender, contextToken, nil, 1); err != nil {
		t.Fatalf("configured model reply did not complete provider-to-channel delivery: %v", err)
	}
	if modelRequests != 1 || sendRequests != 1 {
		t.Fatalf("model/bridge request counts=%d/%d; want one each", modelRequests, sendRequests)
	}
	var assistantCount int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM conversation_messages WHERE conversation_id=UNHEX(?) AND role='assistant' AND content=? AND provider='openai_compatible' AND model='local-provider-test-model'`, conversationID, "reply from configured model").Scan(&assistantCount); err != nil || assistantCount != 1 {
		t.Fatalf("generated reply was not persisted with its provider metadata: count=%d error=%v", assistantCount, err)
	}
	if err = s.DB.QueryRow(`SELECT status,attempts FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?) AND provider_message_id=?`, accountID, messageKey).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || attempts != 2 {
		t.Fatalf("configured provider delivery status=%s attempts=%d; want sent/2", status, attempts)
	}
}

func TestWechatCredentialsRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	ciphertext, err := encryptWechat("channel-secret")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("channel-secret")) {
		t.Fatal("secret present in ciphertext")
	}
	plain, err := decryptWechat(ciphertext)
	if err != nil || plain != "channel-secret" {
		t.Fatal("credentials did not round trip")
	}
}

func TestWechatBindingRechecksActiveMembership(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	s, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	uid, tid, botID := id(), id(), id()
	if _, err = s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, uid, "bind-test-"+uid+"@example.invalid", "Binding Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	defer s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, uid)
	if _, err = s.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Binding Test',?,'active',UNHEX(?),'{}')`, tid, "binding-"+tid, uid); err != nil {
		t.Fatal(err)
	}
	defer s.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, tid)
	if _, err = s.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'member','suspended',UTC_TIMESTAMP(3))`, tid, uid); err != nil {
		t.Fatal(err)
	}
	defer s.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, tid, uid)
	defer s.DB.Exec(`DELETE FROM audit_logs WHERE team_id=UNHEX(?) AND actor_user_id=UNHEX(?)`, tid, uid)
	session := &wechatSession{UserID: uid, TeamID: tid}
	if err = s.storeWechatBinding(session, botID, "scanner-"+uid, "secret"); err == nil {
		t.Fatal("suspended member unexpectedly bound a channel")
	}
	var count int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM channel_accounts WHERE provider_user_hash=?`, hash(botID)).Scan(&count); err != nil || count != 0 {
		t.Fatalf("binding wrote channel account while suspended: count=%d error=%v", count, err)
	}
	if _, err = s.DB.Exec(`UPDATE team_memberships SET status='active' WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, tid, uid); err != nil {
		t.Fatal(err)
	}
	if err = s.storeWechatBinding(session, botID, "scanner-"+uid, "secret"); err != nil {
		t.Fatal(err)
	}
	var cid string
	if err = s.DB.QueryRow(`SELECT HEX(id) FROM channel_accounts WHERE provider_user_hash=?`, hash(botID)).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	defer s.DB.Exec(`DELETE FROM channel_accounts WHERE id=UNHEX(?)`, cid)
	defer s.DB.Exec(`DELETE FROM channel_bindings WHERE channel_account_id=UNHEX(?)`, cid)
}

func TestWechatIdentityClaimUnbindAndRebindLifecycle(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	s, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	firstUserID, firstTeamID := id(), id()
	secondUserID, secondTeamID := id(), id()
	firstToken, secondToken := token(), token()
	botID := "claim-lifecycle-" + id()
	raceBotID := "claim-race-" + id()
	scannerA, scannerB := "scanner-a-"+id(), "scanner-b-"+id()
	firstSecret, secondSecret := "first-channel-secret-"+id(), "second-channel-secret-"+id()
	var channelID string
	t.Cleanup(func() {
		accountHashes := [][]byte{hash(botID), hash(raceBotID)}
		for _, accountHash := range accountHashes {
			s.DB.Exec(`DELETE FROM channel_message_deliveries WHERE channel_account_id IN (SELECT id FROM channel_accounts WHERE provider_user_hash=?)`, accountHash)
			s.DB.Exec(`DELETE FROM channel_conversations WHERE channel_account_id IN (SELECT id FROM channel_accounts WHERE provider_user_hash=?)`, accountHash)
			s.DB.Exec(`DELETE FROM channel_bindings WHERE channel_account_id IN (SELECT id FROM channel_accounts WHERE provider_user_hash=?)`, accountHash)
			s.DB.Exec(`DELETE FROM channel_accounts WHERE provider_user_hash=?`, accountHash)
		}
		s.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id IN (UNHEX(?),UNHEX(?))`, firstUserID, secondUserID)
		s.DB.Exec(`DELETE FROM auth_sessions WHERE user_id IN (UNHEX(?),UNHEX(?))`, firstUserID, secondUserID)
		s.DB.Exec(`DELETE FROM team_memberships WHERE user_id IN (UNHEX(?),UNHEX(?))`, firstUserID, secondUserID)
		s.DB.Exec(`DELETE FROM teams WHERE id IN (UNHEX(?),UNHEX(?))`, firstTeamID, secondTeamID)
		s.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?))`, firstUserID, secondUserID)
		s.DB.Close()
	})
	for _, fixture := range []struct {
		userID, teamID, bearer, suffix string
	}{
		{firstUserID, firstTeamID, firstToken, "first"},
		{secondUserID, secondTeamID, secondToken, "second"},
	} {
		if _, err = s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, fixture.userID, "wechat-claim-"+fixture.userID+"@example.invalid", "WeChat Claim "+fixture.suffix, "not-a-login"); err != nil {
			t.Fatal(err)
		}
		if _, err = s.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),?,?, 'active',UNHEX(?),'{}')`, fixture.teamID, "WeChat Claim "+fixture.suffix, "wechat-claim-"+fixture.teamID, fixture.userID); err != nil {
			t.Fatal(err)
		}
		if _, err = s.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UTC_TIMESTAMP(3))`, fixture.teamID, fixture.userID); err != nil {
			t.Fatal(err)
		}
		if _, err = s.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), fixture.userID, hash(fixture.bearer), time.Now().UTC().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	firstSession := &wechatSession{UserID: firstUserID, TeamID: firstTeamID}
	secondSession := &wechatSession{UserID: secondUserID, TeamID: secondTeamID}
	if err = s.storeWechatBinding(firstSession, botID, scannerA, firstSecret); err != nil {
		t.Fatalf("first binding failed: %v", err)
	}
	if err = s.DB.QueryRow(`SELECT HEX(id) FROM channel_accounts WHERE provider_user_hash=?`, hash(botID)).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	if err = s.storeWechatBinding(secondSession, botID, scannerB, secondSecret); err == nil {
		t.Fatal("a second TeamMind account claimed an actively bound WeChat identity")
	}
	assertBinding := func(wantUser, wantTeam, wantScanner, wantSecret string, wantBound bool) {
		t.Helper()
		var gotUser, gotTeam, gotScanner string
		var gotCiphertext []byte
		var unboundAt sql.NullTime
		err := s.DB.QueryRow(`SELECT HEX(b.user_id),HEX(b.current_team_id),HEX(c.scanner_hash),c.encrypted_credentials,b.unbound_at FROM channel_accounts c JOIN channel_bindings b ON b.channel_account_id=c.id WHERE c.id=UNHEX(?) ORDER BY b.bound_at DESC LIMIT 1`, channelID).Scan(&gotUser, &gotTeam, &gotScanner, &gotCiphertext, &unboundAt)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := decryptWechat(gotCiphertext)
		if err != nil {
			t.Fatal("stored channel credentials could not be decrypted")
		}
		credentials, err := decodeWechatCredentials(plain, "https://ilinkai.weixin.qq.com")
		if err != nil {
			t.Fatal("stored channel credentials could not be decoded")
		}
		if !strings.EqualFold(gotUser, wantUser) || !strings.EqualFold(gotTeam, wantTeam) || !strings.EqualFold(gotScanner, hex.EncodeToString(hash(wantScanner))) || credentials.Token != wantSecret || unboundAt.Valid == wantBound {
			t.Fatalf("binding state mismatch: user=%s team=%s active=%t", gotUser, gotTeam, !unboundAt.Valid)
		}
	}
	assertBinding(firstUserID, firstTeamID, scannerA, firstSecret, true)

	call := func(bearer string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/me/channels/wechat/"+channelID, nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		response := httptest.NewRecorder()
		s.ServeHTTP(response, req)
		return response
	}
	if response := call(firstToken); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"unbound":true`) {
		t.Fatalf("owner unbind failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var unbindAuditCount int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE actor_user_id=UNHEX(?) AND action='wechat.unbind' AND resource_id=UNHEX(?)`, firstUserID, channelID).Scan(&unbindAuditCount); err != nil || unbindAuditCount != 1 {
		t.Fatalf("unbind audit missing or duplicated: count=%d error=%v", unbindAuditCount, err)
	}
	if response := call(firstToken); response.Code != http.StatusNotFound {
		t.Fatalf("repeated unbind status=%d body=%s; want 404", response.Code, response.Body.String())
	}
	if err = s.storeWechatBinding(secondSession, botID, scannerB, secondSecret); err != nil {
		t.Fatalf("rebind after explicit unbind failed: %v", err)
	}
	assertBinding(secondUserID, secondTeamID, scannerB, secondSecret, true)
	if err = s.storeWechatBinding(firstSession, botID, scannerA, firstSecret); err == nil {
		t.Fatal("previous account reclaimed the identity after it was rebound to another account")
	}
	if response := call(firstToken); response.Code != http.StatusNotFound {
		t.Fatalf("former owner unbound the new owner's account: status=%d body=%s", response.Code, response.Body.String())
	}
	assertBinding(secondUserID, secondTeamID, scannerB, secondSecret, true)

	// Two different platform accounts confirming the same previously unseen
	// provider identity at once must still produce exactly one active owner.
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, candidate := range []struct {
		session *wechatSession
		scanner string
		secret  string
	}{
		{firstSession, scannerA, firstSecret},
		{secondSession, scannerB, secondSecret},
	} {
		go func(candidate struct {
			session *wechatSession
			scanner string
			secret  string
		}) {
			<-start
			results <- s.storeWechatBinding(candidate.session, raceBotID, candidate.scanner, candidate.secret)
		}(candidate)
	}
	close(start)
	firstResult, secondResult := <-results, <-results
	if (firstResult == nil) == (secondResult == nil) {
		t.Fatalf("identity race must have exactly one winner: first=%v second=%v", firstResult, secondResult)
	}
	var activeClaims int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM channel_bindings b JOIN channel_accounts c ON c.id=b.channel_account_id WHERE c.provider='openclaw-weixin' AND c.provider_user_hash=? AND b.unbound_at IS NULL`, hash(raceBotID)).Scan(&activeClaims); err != nil || activeClaims != 1 {
		t.Fatalf("concurrent confirmations produced %d active claims; error=%v", activeClaims, err)
	}
}

func TestWechatBindingCredentialsSurviveAPIRestart(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("WECHAT_MODE", "real")

	first, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	userID, teamID, botID := id(), id(), id()
	teamName := "Restart Binding " + teamID
	if _, err = first.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "wechat-restart-"+userID+"@example.invalid", "Restart Binding", "not-a-login"); err != nil {
		first.DB.Close()
		t.Fatal(err)
	}
	if _, err = first.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),?,?,'active',UNHEX(?),'{}')`, teamID, teamName, "restart-"+teamID, userID); err != nil {
		first.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		first.DB.Close()
		t.Fatal(err)
	}
	if _, err = first.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UTC_TIMESTAMP(3))`, teamID, userID); err != nil {
		first.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		first.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		first.DB.Close()
		t.Fatal(err)
	}
	session := &wechatSession{UserID: userID, TeamID: teamID}
	const channelSecret = "restart-test-channel-secret"
	const scannerID = "restart-test-scanner"
	if err = first.storeWechatBindingWithBaseURL(session, botID, scannerID, channelSecret, "https://hk.weixin.qq.com"); err != nil {
		first.DB.Close()
		t.Fatal(err)
	}
	var channelID string
	var rawCiphertext []byte
	if err = first.DB.QueryRow(`SELECT HEX(c.id),c.encrypted_credentials FROM channel_accounts c JOIN channel_bindings b ON b.channel_account_id=c.id WHERE b.user_id=UNHEX(?) AND b.unbound_at IS NULL`, userID).Scan(&channelID, &rawCiphertext); err != nil {
		first.DB.Close()
		t.Fatal(err)
	}
	if bytes.Contains(rawCiphertext, []byte(channelSecret)) {
		first.DB.Close()
		t.Fatal("channel credential was stored in plaintext")
	}
	if err = first.DB.Close(); err != nil {
		t.Fatal(err)
	}

	// NewServer constructs a fresh API process state. The binding and encrypted
	// credential must be recoverable from MySQL without any in-memory session.
	restarted, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		restarted.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id=UNHEX(?)`, userID)
		restarted.DB.Exec(`DELETE FROM channel_bindings WHERE channel_account_id=UNHEX(?)`, channelID)
		restarted.DB.Exec(`DELETE FROM channel_accounts WHERE id=UNHEX(?)`, channelID)
		restarted.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, userID)
		restarted.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		restarted.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		restarted.DB.Close()
	})
	var gotChannelID, provider, status, gotUserID, gotTeamID, providerBaseURL string
	var scannerHash, ciphertext []byte
	err = restarted.DB.QueryRow(`SELECT HEX(c.id),c.provider,c.status,HEX(b.user_id),HEX(b.current_team_id),c.scanner_hash,c.provider_base_url,c.encrypted_credentials FROM channel_accounts c JOIN channel_bindings b ON b.channel_account_id=c.id WHERE c.id=UNHEX(?) AND b.unbound_at IS NULL`, channelID).Scan(&gotChannelID, &provider, &status, &gotUserID, &gotTeamID, &scannerHash, &providerBaseURL, &ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := decryptWechat(ciphertext)
	credentials, decodeErr := decodeWechatCredentials(secret, providerBaseURL)
	if err != nil || decodeErr != nil || credentials.Token != channelSecret {
		t.Fatalf("restarted API could not recover channel credential: secret recovered=%t decrypt=%v decode=%v", credentials.Token == channelSecret, err, decodeErr)
	}
	if !strings.EqualFold(gotChannelID, channelID) || provider != "openclaw-weixin" || status != "active" || !strings.EqualFold(gotUserID, userID) || !strings.EqualFold(gotTeamID, teamID) || !bytes.Equal(scannerHash, hash(scannerID)) || providerBaseURL != "https://hk.weixin.qq.com" || credentials.BaseURL != providerBaseURL {
		t.Fatalf("restarted binding lost identity scope: channel=%s provider=%s status=%s user=%s team=%s base=%s", gotChannelID, provider, status, gotUserID, gotTeamID, providerBaseURL)
	}
	var userCount int
	if err = restarted.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE id=UNHEX(?) AND status='active'`, userID).Scan(&userCount); err != nil || userCount != 1 {
		t.Fatalf("restarted account lookup failed: count=%d err=%v", userCount, err)
	}
}

func TestWechatPluginConfirmationPersistsTheConfirmedIdentity(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("WECHAT_MODE", "real")
	const bridgeToken = "integration-bridge-token-that-is-at-least-32-bytes"
	t.Setenv("WECHAT_GATEWAY_BRIDGE_TOKEN", bridgeToken)

	const sessionID = "cccccccccccccccccccccccccccccccc"
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/teammind-wechat-bridge/v1/sessions/poll" || r.Method != http.MethodPost {
			t.Errorf("unexpected plugin bridge request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+bridgeToken {
			t.Errorf("plugin bridge request was not authenticated")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input["sessionKey"] != sessionID {
			t.Errorf("plugin bridge received the wrong login session")
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"confirmed","accountId":"plugin-confirmed-account","userId":"plugin-confirmed-scanner","botToken":"plugin-confirmed-secret","baseUrl":"https://hk.weixin.qq.com"}`))
	}))
	defer bridge.Close()
	t.Setenv("WECHAT_GATEWAY_URL", bridge.URL+"/teammind-wechat-bridge/v1")

	s, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	userID, teamID := id(), id()
	if _, err = s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, userID, "plugin-confirm-"+userID+"@example.invalid", "Plugin Confirmation", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),?,?, 'active',UNHEX(?),'{}')`, teamID, "Plugin Confirmation "+teamID, "plugin-confirm-"+teamID, userID); err != nil {
		s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status,joined_at) VALUES(UNHEX(?),UNHEX(?),'team_owner','active',UTC_TIMESTAMP(3))`, teamID, userID); err != nil {
		s.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.DB.Exec(`DELETE FROM audit_logs WHERE actor_user_id=UNHEX(?)`, userID)
		s.DB.Exec(`DELETE FROM channel_bindings WHERE user_id=UNHEX(?)`, userID)
		s.DB.Exec(`DELETE FROM channel_accounts WHERE provider_user_hash=?`, hash("plugin-confirmed-account"))
		s.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?)`, teamID, userID)
		s.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
		s.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, userID)
		s.DB.Close()
	})

	session := &wechatSession{ID: sessionID, UserID: userID, TeamID: teamID, Expires: time.Now().UTC().Add(time.Minute), Gateway: true}
	session.state.Store(&wechatSessionState{Status: "scaned"})
	s.pollWechatGateway(session)
	state := session.snapshot()
	if !state.Done || state.Status != "confirmed" {
		t.Fatalf("confirmed plugin response did not finish TeamMind session: status=%s done=%t error=%s", state.Status, state.Done, state.Error)
	}
	var provider, scannerHash, team string
	var ciphertext []byte
	err = s.DB.QueryRow(`SELECT c.provider,HEX(b.current_team_id),HEX(c.scanner_hash),c.encrypted_credentials FROM channel_accounts c JOIN channel_bindings b ON b.channel_account_id=c.id WHERE c.provider_user_hash=? AND b.user_id=UNHEX(?) AND b.unbound_at IS NULL`, hash("plugin-confirmed-account"), userID).Scan(&provider, &team, &scannerHash, &ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptWechat(ciphertext)
	if err != nil {
		t.Fatal("confirmed plugin credential was not encrypted")
	}
	var stored wechatCredentials
	if err = json.Unmarshal([]byte(plain), &stored); err != nil {
		t.Fatal(err)
	}
	decodedScannerHash, err := hex.DecodeString(scannerHash)
	if err != nil {
		t.Fatal("stored scanner identity hash was invalid")
	}
	if provider != "openclaw-weixin" || !strings.EqualFold(team, teamID) || !bytes.Equal(decodedScannerHash, hash("plugin-confirmed-scanner")) || stored.Token != "plugin-confirmed-secret" || stored.BaseURL != "https://hk.weixin.qq.com" {
		t.Fatalf("TeamMind did not persist the confirmed plugin identity and scope")
	}
	if bytes.Contains(ciphertext, []byte("plugin-confirmed-secret")) {
		t.Fatal("confirmed plugin token was stored in plaintext")
	}
}

// Run with MYSQL_DSN against a migrated test database. A rollback removes all
// fixtures, while the assertions exercise the real SQL authorization boundary.
func TestWechatInboundSQLIsolation(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WECHAT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	s, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	uid, tid, cid := id(), id(), id()
	if _, err = tx.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, uid, "wechat-test-"+uid+"@example.invalid", "Worker Test", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'Worker Test',?,'active',UNHEX(?),'{}')`, tid, "worker-"+tid, uid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status) VALUES(UNHEX(?),UNHEX(?),'team_owner','active')`, tid, uid); err != nil {
		t.Fatal(err)
	}
	secret, _ := encryptWechat("fake-test-token")
	if _, err = tx.Exec(`INSERT INTO channel_accounts(id,provider,provider_user_hash,scanner_hash,encrypted_credentials,status) VALUES(UNHEX(?),'openclaw-weixin',?,?,?,'active')`, cid, hash("test-bot"), hash("known-sender"), secret); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO channel_bindings(channel_account_id,user_id,current_team_id) VALUES(UNHEX(?),UNHEX(?),UNHEX(?))`, cid, uid, tid); err != nil {
		t.Fatal(err)
	}
	makeMessage := func(mid, from string) pluginWechatMessage {
		var m pluginWechatMessage
		m.MessageID = json.Number(mid)
		m.From = from
		m.MessageType = 1
		m.Items = make([]struct {
			Type int `json:"type"`
			Text struct {
				Text string `json:"text"`
			} `json:"text_item"`
		}, 1)
		m.Items[0].Type = 1
		m.Items[0].Text.Text = "hello"
		return m
	}
	if err = s.recordChannelMessage(ctx, tx, cid, makeMessage("1", "stranger")); err != nil {
		t.Fatal(err)
	}
	if err = s.recordChannelMessage(ctx, tx, cid, makeMessage("2", "known-sender")); err != nil {
		t.Fatal(err)
	}
	if err = s.recordChannelMessage(ctx, tx, cid, makeMessage("2", "known-sender")); err != nil {
		t.Fatal(err)
	}
	var rejected, pending int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?) AND status='rejected'`, cid).Scan(&rejected); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?) AND status='pending'`, cid).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if rejected != 1 || pending != 1 {
		t.Fatalf("unexpected delivery counts rejected=%d pending=%d", rejected, pending)
	}
	var count int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM channel_conversations WHERE channel_account_id=UNHEX(?) AND user_id=UNHEX(?) AND team_id=UNHEX(?)`, cid, uid, tid).Scan(&count); err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected isolated conversation; got %d", count)
	}
}

func TestAITeamContextOnlyIncludesConfirmedRelevantTeamData(t *testing.T) {
	if os.Getenv("MYSQL_DSN") == "" {
		t.Skip("set MYSQL_DSN for database integration")
	}
	s, err := NewServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	userID, otherID, teamID, projectID, privateMemoryID := id(), id(), id(), id(), id()
	defer s.DB.Exec(`DELETE FROM users WHERE id IN (UNHEX(?),UNHEX(?))`, userID, otherID)
	defer s.DB.Exec(`DELETE FROM teams WHERE id=UNHEX(?)`, teamID)
	defer s.DB.Exec(`DELETE FROM projects WHERE id=UNHEX(?)`, projectID)
	defer s.DB.Exec(`DELETE FROM team_memories WHERE team_id=UNHEX(?)`, teamID)
	defer s.DB.Exec(`DELETE FROM user_memories WHERE id=UNHEX(?)`, privateMemoryID)
	defer s.DB.Exec(`DELETE FROM team_memberships WHERE team_id=UNHEX(?)`, teamID)
	defer s.DB.Exec(`DELETE FROM tasks WHERE team_id=UNHEX(?)`, teamID)
	if _, err = s.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?),(UNHEX(?),?,?,?)`, userID, "ai-context-"+userID+"@example.invalid", "AI Context", "not-a-login", otherID, "ai-context-other-"+otherID+"@example.invalid", "Other", "not-a-login"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO teams(id,name,slug,status,created_by,settings) VALUES(UNHEX(?),'AI Context Team',?,'active',UNHEX(?),'{}')`, teamID, "ai-context-"+teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO team_memberships(team_id,user_id,role,status) VALUES(UNHEX(?),UNHEX(?),'team_owner','active')`, teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO team_memories(id,team_id,created_by,category,content,sensitivity,confirmed_at) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'decision','Confirmed shared policy','normal',UTC_TIMESTAMP(3)),(UNHEX(?),UNHEX(?),UNHEX(?),'secret','Restricted shared secret','restricted',UTC_TIMESTAMP(3)),(UNHEX(?),UNHEX(?),UNHEX(?),'draft','Unconfirmed draft','normal',NULL)`, id(), teamID, userID, id(), teamID, userID, id(), teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO user_memories(id,owner_user_id,category,content) VALUES(UNHEX(?),UNHEX(?),'profile','Private personal memory')`, privateMemoryID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO projects(id,team_id,name,description,status,created_by) VALUES(UNHEX(?),UNHEX(?),'Context project','','active',UNHEX(?))`, projectID, teamID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO tasks(id,team_id,project_id,title,description,created_by,assignee_id) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'My assigned task','',UNHEX(?),UNHEX(?)),(UNHEX(?),UNHEX(?),UNHEX(?),'Another member private task','',UNHEX(?),UNHEX(?))`, id(), teamID, projectID, userID, userID, id(), teamID, projectID, otherID, otherID); err != nil {
		t.Fatal(err)
	}
	teamContext, err := s.aiTeamContext(ctx, teamID, userID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(teamContext)
	if err != nil {
		t.Fatal(err)
	}
	content := string(encoded)
	for _, expected := range []string{"AI Context Team", "Confirmed shared policy", "My assigned task"} {
		if !strings.Contains(content, expected) {
			t.Errorf("team context omitted %q: %s", expected, content)
		}
	}
	for _, forbidden := range []string{"Restricted shared secret", "Unconfirmed draft", "Private personal memory", "Another member private task"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("team context included unauthorized data %q", forbidden)
		}
	}
}
