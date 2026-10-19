package live

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Server struct {
	DB              *sql.DB
	origins         map[string]struct{}
	development     bool
	wechat          *wechatService
	firebase        *firebaseMessagingSender
	loginRateKey    []byte
	trustedProxies  []*net.IPNet
	rateCleanupMu   sync.Mutex
	lastRateCleanup time.Time
}
type User struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	Timezone   string `json:"timezone"`
	SuperAdmin bool   `json:"superAdmin"`
}
type envelope struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data"`
}
type failure struct {
	status        int
	code, message string
}

func (e failure) Error() string                  { return e.code }
func bad(status int, code, message string) error { return failure{status, code, message} }
func write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Code: "OK", Data: data})
}
func writeErr(w http.ResponseWriter, e error) {
	var f failure
	if !errors.As(e, &f) {
		log.Printf("internal API error: %v", e)
		f = failure{500, "INTERNAL_ERROR", "服务器处理失败，请稍后重试"}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(f.status)
	_ = json.NewEncoder(w).Encode(envelope{Code: f.code, Message: f.message})
}
func body(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return bad(400, "INVALID_REQUEST", "请求参数格式无效")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return bad(400, "INVALID_REQUEST", "请求体只能包含一个 JSON 对象")
	}
	return nil
}
func id() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func hash(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }
func validID(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func method(r *http.Request, m string) error {
	if r.Method != m {
		return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
	}
	return nil
}

func parseAllowedOrigins(raw string) map[string]struct{} {
	origins := make(map[string]struct{})
	for _, origin := range strings.Split(raw, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return origins
}

func NewServer() (*Server, error) {
	proxyCIDRs, e := parseTrustedProxyCIDRs(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if e != nil {
		return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS: %w", e)
	}
	rateKey, e := decodeRateLimitKey(os.Getenv("LOGIN_RATE_LIMIT_HMAC_KEY"), os.Getenv("APP_ENV") == "production")
	if e != nil {
		return nil, e
	}
	raw := os.Getenv("MYSQL_DSN")
	if raw == "" {
		return nil, errors.New("MYSQL_DSN is required; no in-memory fallback")
	}
	cfg, e := mysql.ParseDSN(raw)
	if e != nil {
		return nil, e
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	db, e := sql.Open("mysql", cfg.FormatDSN())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	if e = db.Ping(); e != nil {
		db.Close()
		return nil, e
	}
	var migrated int
	if e = db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='000021_membership_versions'`).Scan(&migrated); e != nil || migrated != 1 {
		db.Close()
		return nil, errors.New("MySQL schema is not at version 000021_membership_versions; run scripts/migrate.sh against an empty or versioned database")
	}
	server := &Server{DB: db, origins: parseAllowedOrigins(os.Getenv("CORS_ORIGIN")), development: os.Getenv("APP_ENV") != "production", loginRateKey: rateKey, trustedProxies: proxyCIDRs, firebase: newFirebaseMessagingSender()}
	server.wechat = newWechatService()
	return server, nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Cache-Control", "no-store")
	origin := r.Header.Get("Origin")
	if _, ok := s.origins[origin]; origin != "" && ok {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
	}
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.URL.Path == "/healthz" {
		write(w, 200, map[string]any{"status": "ok"})
		return
	}
	if r.URL.Path == "/readyz" {
		var migrated int
		if e := s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM schema_migrations WHERE version='000021_membership_versions'").Scan(&migrated); e != nil || migrated != 1 {
			writeErr(w, bad(503, "DATABASE_UNAVAILABLE", "数据库未就绪"))
		} else {
			write(w, 200, map[string]any{"status": "ready"})
		}
		return
	}
	// Bound the whole application API by client source in addition to the
	// stricter per-route protections on login, recovery, MFA, and email change.
	// Health/readiness and preflight requests stay available to probes/browsers.
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		if e := s.enforceRequestRateLimit(r, "api"); e != nil {
			writeErr(w, e)
			return
		}
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		writeErr(w, bad(404, "NOT_FOUND", "资源不存在"))
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	// This endpoint is authorized by a short-lived, single-use mailbox token.
	// Do not require the old session: email links may be opened on another
	// device or after the original session has expired.
	if path == "me/email-change/confirm" {
		if e := s.confirmEmailChange(w, r); e != nil {
			writeErr(w, e)
		}
		return
	}
	if path == "auth/mfa/complete" {
		if e := s.enforceRequestRateLimit(r, "mfa-complete"); e != nil {
			writeErr(w, e)
			return
		}
		if e := s.completeMFALogin(w, r); e != nil {
			writeErr(w, e)
		}
		return
	}
	if strings.HasPrefix(path, "auth/") {
		if r.Method == http.MethodPost {
			if e := s.enforceRequestRateLimit(r, strings.TrimPrefix(path, "auth/")); e != nil {
				writeErr(w, e)
				return
			}
		}
		if e := s.auth(w, r, strings.TrimPrefix(path, "auth/")); e != nil {
			writeErr(w, e)
		}
		return
	}
	u, e := s.currentUser(r)
	if e != nil {
		writeErr(w, e)
		return
	}
	var result error
	switch {
	case path == "me":
		if r.Method == "GET" {
			write(w, 200, u)
		} else {
			result = s.profile(w, r, u)
		}
	case path == "me/data-export":
		result = s.exportPersonalData(w, r, u)
	case path == "me/account":
		result = s.deleteAccount(w, r, u)
	case path == "me/email-change":
		result = s.requestEmailChange(w, r, u)
	case path == "me/mfa" || strings.HasPrefix(path, "me/mfa/"):
		result = s.mfaRoute(w, r, u, path)
	case path == "me/teams" || path == "teams":
		result = s.teams(w, r, u, path)
	case path == "invitations/accept":
		result = s.acceptInvitation(w, r, u)
	case strings.HasPrefix(path, "teams/"):
		result = s.teamRoute(w, r, u, strings.Split(path, "/"))
	case path == "me/memories" || strings.HasPrefix(path, "me/memories/"):
		result = s.memories(w, r, u, path)
	case path == "me/notifications" || strings.HasPrefix(path, "me/notifications/"):
		result = s.notifications(w, r, u, path)
	case path == "me/notification-deliveries":
		result = s.notificationDeliveries(w, r, u)
	case path == "me/reminders" || strings.HasPrefix(path, "me/reminders/"):
		result = s.reminders(w, r, u, path)
	case path == "me/notification-preferences":
		result = s.notificationPreferences(w, r, u)
	case path == "me/notification-channels" || strings.HasPrefix(path, "me/notification-channels/"):
		if strings.HasPrefix(path, "me/notification-channels/native-devices") {
			result = s.nativeDevices(w, r, u, path)
		} else {
			result = s.notificationChannels(w, r, u, path)
		}
	case strings.HasPrefix(path, "me/channels/wechat"):
		result = s.wechatRoute(w, r, u, path)
	case path == "me/conversations" || strings.HasPrefix(path, "me/conversations/"):
		result = s.conversations(w, r, u, path)
	case path == "me/ai/status":
		if e := method(r, "GET"); e != nil {
			result = e
		} else {
			provider := os.Getenv("AI_PROVIDER")
			if provider == "" {
				provider = "mock"
			}
			configured := provider == "mock" || provider == "openai_compatible" && os.Getenv("AI_BASE_URL") != "" && os.Getenv("AI_MODEL") != "" && os.Getenv("AI_API_KEY") != ""
			_, workspaceKeyErr := agentWorkspaceKey()
			write(w, 200, map[string]any{"provider": provider, "configured": configured, "simulated": provider == "mock", "agentTools": provider == "openai_compatible" && configured, "sandboxAvailable": agentSandboxAvailable(), "shellAvailable": agentShellAvailable(), "workspaceAvailable": workspaceKeyErr == nil})
		}
	case path == "me/agent/workspace/files" || strings.HasPrefix(path, "me/agent/workspace/files/"):
		result = s.agentWorkspaceFilesRoute(w, r, u, strings.Split(path, "/"))
	case strings.HasPrefix(path, "me/agent/"):
		result = s.agentTools(w, r, u, path)
	case strings.HasPrefix(path, "platform/"):
		result = s.platform(w, r, u, path)
	default:
		result = bad(404, "NOT_FOUND", "资源不存在")
	}
	if result != nil {
		writeErr(w, result)
	}
}
func (s *Server) currentUser(r *http.Request) (User, error) {
	a := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if a == "" || a == r.Header.Get("Authorization") {
		return User{}, bad(401, "UNAUTHENTICATED", "请先登录")
	}
	var u User
	var flag int
	e := s.DB.QueryRowContext(r.Context(), `SELECT HEX(u.id),u.email,u.display_name,u.timezone,EXISTS(SELECT 1 FROM platform_roles p WHERE p.user_id=u.id AND p.role='super_admin') FROM auth_sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.expires_at>UTC_TIMESTAMP(3) AND u.status='active'`, hash(a)).Scan(&u.ID, &u.Email, &u.Name, &u.Timezone, &flag)
	if e == sql.ErrNoRows {
		return User{}, bad(401, "SESSION_EXPIRED", "会话已过期，请重新登录")
	}
	if e != nil {
		return User{}, e
	}
	u.SuperAdmin = flag == 1
	u.Role = "member"
	if u.SuperAdmin {
		u.Role = "super_admin"
	}
	return u, nil
}
