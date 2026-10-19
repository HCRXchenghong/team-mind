package live

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestClientIPUsesOnlyTrustedProxyHeaders(t *testing.T) {
	_, trustedNetwork, err := net.ParseCIDR("10.20.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	trusted := []*net.IPNet{trustedNetwork}
	tests := []struct {
		name       string
		remote     string
		forwarded  string
		trustProxy bool
		want       string
		wantErr    bool
	}{
		{name: "ignore spoofed header from public peer", remote: "198.51.100.8:9000", forwarded: "203.0.113.4", want: "198.51.100.8"},
		{name: "take client before trusted proxy chain", remote: "10.20.0.9:9000", forwarded: "203.0.113.4, 10.20.0.8", trustProxy: true, want: "203.0.113.4"},
		{name: "invalid trusted header is rejected", remote: "10.20.0.9:9000", forwarded: "spoofed", trustProxy: true, wantErr: true},
		{name: "invalid remote address is rejected", remote: "not-an-ip", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.RemoteAddr = test.remote
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			proxyConfig := []*net.IPNet(nil)
			if test.trustProxy {
				proxyConfig = trusted
			}
			got, gotErr := requestClientIP(request, proxyConfig)
			if (gotErr != nil) != test.wantErr {
				t.Fatalf("error=%v, wantErr=%v", gotErr, test.wantErr)
			}
			if !test.wantErr && got.String() != test.want {
				t.Fatalf("client ip=%q, want %q", got, test.want)
			}
		})
	}
}

func TestRateLimitKeyProductionConfiguration(t *testing.T) {
	if _, err := decodeRateLimitKey("", true); err == nil {
		t.Fatal("production must reject the development fallback key")
	}
	if _, err := decodeRateLimitKey(strings.Repeat("a", 62), true); err == nil {
		t.Fatal("production must reject a key shorter than 32 bytes")
	}
	key, err := decodeRateLimitKey(strings.Repeat("ab", 32), true)
	if err != nil || len(key) != 32 {
		t.Fatalf("valid 32-byte hex key rejected: length=%d error=%v", len(key), err)
	}
	if _, err := parseTrustedProxyCIDRs("127.0.0.1/32,invalid"); err == nil {
		t.Fatal("invalid proxy CIDR must fail startup configuration")
	}
}

func TestLoginIPRateLimitAndPlatformAlert(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	prefix := id()
	adminID, adminSecret := id(), token()
	peerIP := "198.51.100.79"
	defer func() {
		server.DB.Exec(`DELETE FROM auth_ip_rate_limits WHERE scope_hash=?`, loginScopeHash(server.loginRateKey, "login", peerIP))
		server.DB.Exec(`DELETE FROM login_failures WHERE email LIKE ?`, "rate-"+prefix+"%@example.invalid")
		server.DB.Exec(`DELETE FROM auth_sessions WHERE user_id=UNHEX(?)`, adminID)
		server.DB.Exec(`DELETE FROM platform_roles WHERE user_id=UNHEX(?)`, adminID)
		server.DB.Exec(`DELETE FROM users WHERE id=UNHEX(?)`, adminID)
		server.DB.Close()
	}()
	if _, err = server.DB.Exec(`INSERT INTO users(id,email,display_name,password_hash) VALUES(UNHEX(?),?,?,?)`, adminID, "rate-admin-"+prefix+"@example.invalid", "Rate Admin", "unused"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO platform_roles(user_id,role) VALUES(UNHEX(?),'super_admin')`, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.DB.Exec(`INSERT INTO auth_sessions(id,user_id,token_hash,expires_at) VALUES(UNHEX(?),UNHEX(?),?,?)`, id(), adminID, hash(adminSecret), time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < authRatePolicies["login"].limit; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"email":"rate-%s-%02d@example.invalid","password":"wrong-password"}`, prefix, i)))
		request.RemoteAddr = peerIP + ":4100"
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("failed login %d status=%d body=%s", i+1, response.Code, response.Body.String())
		}
	}
	blocked := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"blocked@example.invalid","password":"wrong-password"}`))
	blocked.RemoteAddr = peerIP + ":4101"
	blocked.Header.Set("Content-Type", "application/json")
	blockedResponse := httptest.NewRecorder()
	server.ServeHTTP(blockedResponse, blocked)
	if blockedResponse.Code != http.StatusTooManyRequests || !strings.Contains(blockedResponse.Body.String(), "AUTH_RATE_LIMITED") {
		t.Fatalf("expected IP limit, status=%d body=%s", blockedResponse.Code, blockedResponse.Body.String())
	}

	alertRequest := httptest.NewRequest(http.MethodGet, "/api/v1/platform/security-alerts", nil)
	alertRequest.Header.Set("Authorization", "Bearer "+adminSecret)
	alertResponse := httptest.NewRecorder()
	server.ServeHTTP(alertResponse, alertRequest)
	if alertResponse.Code != http.StatusOK {
		t.Fatalf("alerts status=%d body=%s", alertResponse.Code, alertResponse.Body.String())
	}
	var envelope struct {
		Data []struct {
			SourceRef    string `json:"sourceRef"`
			RequestCount int    `json:"requestCount"`
		} `json:"data"`
	}
	if err = json.Unmarshal(alertResponse.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	expectedSourceRef := strings.ToUpper(hex.EncodeToString(loginScopeHash(server.loginRateKey, "login", peerIP)[:8]))
	foundExpectedAlert := false
	for _, alert := range envelope.Data {
		if alert.SourceRef == expectedSourceRef && alert.RequestCount == authRatePolicies["login"].limit {
			foundExpectedAlert = true
			break
		}
	}
	if !foundExpectedAlert || strings.Contains(alertResponse.Body.String(), peerIP) {
		t.Fatalf("unexpected or deanonymized platform alert: %s", alertResponse.Body.String())
	}
}

func TestAPIRateLimitCoversBusinessRoutesAndLeavesHealthChecksAvailable(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	defer server.DB.Close()
	peerIP := "198.51.100.181"
	scope := loginScopeHash(server.loginRateKey, "api", peerIP)
	defer server.DB.Exec(`DELETE FROM auth_ip_rate_limits WHERE scope_hash=?`, scope)
	if _, err = server.DB.Exec(`INSERT INTO auth_ip_rate_limits(scope_hash,window_started_at,request_count) VALUES(?,?,?) ON DUPLICATE KEY UPDATE window_started_at=VALUES(window_started_at),request_count=VALUES(request_count),blocked_until=NULL`, scope, time.Now().UTC(), authRatePolicies["api"].limit); err != nil {
		t.Fatal(err)
	}
	call := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.RemoteAddr = peerIP + ":4200"
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response
	}
	if response := call("/api/v1/me"); response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "API_RATE_LIMITED") {
		t.Fatalf("business API was not rate limited: %d %s", response.Code, response.Body.String())
	}
	if response := call("/api/v1/platform/audit-log"); response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "API_RATE_LIMITED") {
		t.Fatalf("limit did not cover another API route: %d %s", response.Code, response.Body.String())
	}
	if response := call("/healthz"); response.Code != http.StatusOK {
		t.Fatalf("API abuse rate limit blocked health probe: %d %s", response.Code, response.Body.String())
	}
}

func TestConcurrentFirstRequestsShareOneRateLimitCounter(t *testing.T) {
	server, err := NewServer()
	if err != nil {
		t.Skipf("MySQL integration test requires MYSQL_DSN: %v", err)
	}
	defer server.DB.Close()
	peerIP := "198.51.100.182"
	scope := loginScopeHash(server.loginRateKey, "api", peerIP)
	defer server.DB.Exec(`DELETE FROM auth_ip_rate_limits WHERE scope_hash=?`, scope)
	const requests = 48
	var wait sync.WaitGroup
	results := make(chan int, requests)
	for i := 0; i < requests; i++ {
		wait.Add(1)
		go func(port int) {
			defer wait.Done()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/me/notifications", nil)
			request.RemoteAddr = fmt.Sprintf("%s:%d", peerIP, 4300+port)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			results <- response.Code
		}(i)
	}
	wait.Wait()
	close(results)
	for status := range results {
		if status != http.StatusUnauthorized {
			t.Fatalf("concurrent unauthenticated request returned %d; want 401", status)
		}
	}
	var count int
	if err = server.DB.QueryRow(`SELECT request_count FROM auth_ip_rate_limits WHERE scope_hash=?`, scope).Scan(&count); err != nil {
		t.Fatalf("read concurrent request count: %v", err)
	}
	if count != requests {
		t.Fatalf("rate-limit counter=%d after %d first requests; want exact count", count, requests)
	}
}
