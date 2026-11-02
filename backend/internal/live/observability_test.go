package live

import "testing"

func TestRouteKeyNormalizesIDsAndShareTokens(t *testing.T) {
	if got := routeKey("GET", "/api/v1/teams/0123456789ABCDEF0123456789ABCDEF/tasks"); got != "GET /api/v1/teams/:id/tasks" {
		t.Fatalf("route not normalized: %s", got)
	}
	if got := routeKey("GET", "/api/v1/public/gantt/abcdefghijklmnop"); got != "GET /api/v1/public/gantt/:token" {
		t.Fatalf("share token not normalized: %s", got)
	}
	if got := routeKey("GET", "/api/v1/me"); got != "GET /api/v1/me" {
		t.Fatalf("plain route changed: %s", got)
	}
}
