package live

import (
	"net/http/httptest"
	"testing"
)

func TestConfiguredWebAndNativeOriginsAreMatchedExactly(t *testing.T) {
	server := &Server{origins: parseAllowedOrigins("https://app.example.com, capacitor://localhost, https://localhost, http://localhost")}
	for _, origin := range []string{"https://app.example.com", "capacitor://localhost", "https://localhost", "http://localhost"} {
		request := httptest.NewRequest("OPTIONS", "/api/v1/me", nil)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("origin %q allowed as %q", origin, got)
		}
	}
	request := httptest.NewRequest("OPTIONS", "/api/v1/me", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unconfigured origin received CORS permission %q", got)
	}
}
