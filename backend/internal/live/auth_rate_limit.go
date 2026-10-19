package live

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type authRatePolicy struct {
	limit  int
	window time.Duration
}

var authRatePolicies = map[string]authRatePolicy{
	// The API-wide per-source ceiling bounds ordinary authenticated and public
	// endpoints as well as the narrower authentication policies below. The
	// narrower limits still apply to login, recovery, MFA, and email changes.
	"api":                  {limit: 1200, window: time.Minute},
	"login":                {limit: 30, window: time.Minute},
	"forgot":               {limit: 5, window: 15 * time.Minute},
	"reset":                {limit: 10, window: 15 * time.Minute},
	"register":             {limit: 10, window: time.Hour},
	"email-change-request": {limit: 5, window: 15 * time.Minute},
	"email-change-confirm": {limit: 10, window: 15 * time.Minute},
	"mfa-complete":         {limit: 10, window: 15 * time.Minute},
	"mfa-setup":            {limit: 3, window: 15 * time.Minute},
	"mfa-enable":           {limit: 10, window: 15 * time.Minute},
	"mfa-disable":          {limit: 10, window: 15 * time.Minute},
}

func parseTrustedProxyCIDRs(value string) ([]*net.IPNet, error) {
	var result []*net.IPNet
	for _, raw := range strings.Split(value, ",") {
		cidr := strings.TrimSpace(raw)
		if cidr == "" {
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", cidr, err)
		}
		result = append(result, network)
	}
	return result, nil
}

func decodeRateLimitKey(value string, production bool) ([]byte, error) {
	if value == "" {
		if production {
			return nil, fmt.Errorf("LOGIN_RATE_LIMIT_HMAC_KEY is required in production")
		}
		return []byte("teammind-development-only-rate-limit-key"), nil
	}
	key, err := hex.DecodeString(value)
	if err != nil || len(key) < 32 {
		return nil, fmt.Errorf("LOGIN_RATE_LIMIT_HMAC_KEY must be at least 64 hexadecimal characters")
	}
	return key, nil
}

func requestClientIP(r *http.Request, trusted []*net.IPNet) (net.IP, error) {
	peerHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peerHost = r.RemoteAddr
	}
	peer := net.ParseIP(strings.Trim(peerHost, "[]"))
	if peer == nil {
		return nil, fmt.Errorf("invalid remote address")
	}
	peer = normalizeIP(peer)
	if !ipInNetworks(peer, trusted) {
		return peer, nil
	}
	forwardedHeader := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwardedHeader == "" {
		return peer, nil
	}
	forwarded := strings.Split(forwardedHeader, ",")
	chain := make([]net.IP, 0, len(forwarded))
	for _, entry := range forwarded {
		address := net.ParseIP(strings.TrimSpace(entry))
		if address == nil {
			return nil, fmt.Errorf("invalid forwarded address")
		}
		chain = append(chain, normalizeIP(address))
	}
	current := peer
	for i := len(chain) - 1; i >= 0; i-- {
		if !ipInNetworks(current, trusted) {
			return current, nil
		}
		current = chain[i]
	}
	return current, nil
}

func normalizeIP(address net.IP) net.IP {
	if ipv4 := address.To4(); ipv4 != nil {
		return ipv4
	}
	return address.To16()
}

func ipInNetworks(address net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(address) {
			return true
		}
	}
	return false
}

func loginScopeHash(key []byte, route string, clientIP string) []byte {
	digest := hmac.New(sha256.New, key)
	_, _ = digest.Write([]byte(route + "\x00" + clientIP))
	return digest.Sum(nil)
}

func (s *Server) enforceRequestRateLimit(r *http.Request, route string) error {
	policy, ok := authRatePolicies[route]
	if !ok {
		return nil
	}
	clientIP, err := requestClientIP(r, s.trustedProxies)
	if err != nil {
		return bad(http.StatusBadRequest, "INVALID_CLIENT_ADDRESS", "无法识别请求来源，请联系管理员")
	}
	scopeHash := loginScopeHash(s.loginRateKey, route, clientIP.String())
	for attempt := 0; attempt < 4; attempt++ {
		err = s.enforceRequestRateLimitOnce(r.Context(), route, scopeHash, policy)
		if err == nil || isRateLimitRejection(err) {
			s.maybePruneAuthRateLimits(r.Context())
			return err
		}
		if !isRetryableRateLimitLockError(err) || attempt == 3 {
			return err
		}
		delay := time.Duration(5*(1<<attempt)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-r.Context().Done():
			timer.Stop()
			return r.Context().Err()
		case <-timer.C:
		}
	}
	return errors.New("rate limit transaction retry exhausted")
}

func (s *Server) enforceRequestRateLimitOnce(ctx context.Context, route string, scopeHash []byte, policy authRatePolicy) error {
	// Create the per-source row in its own autocommit statement. Performing this
	// upsert inside the locking transaction lets simultaneous first requests
	// acquire overlapping unique-index/gap locks and deadlock before they reach
	// the row lock below (for example, parallel calls made by the notifications
	// page). Once the row exists, serialize updates with SELECT ... FOR UPDATE.
	if _, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO auth_ip_rate_limits(scope_hash,window_started_at,request_count) VALUES(?,UTC_TIMESTAMP(3),0)`, scopeHash); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var startedAt, now time.Time
	var count uint32
	var blockedUntil sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT window_started_at,request_count,blocked_until,UTC_TIMESTAMP(3) FROM auth_ip_rate_limits WHERE scope_hash=? FOR UPDATE`, scopeHash).Scan(&startedAt, &count, &blockedUntil, &now); err != nil {
		return err
	}
	if blockedUntil.Valid && blockedUntil.Time.After(now) {
		if err = tx.Commit(); err != nil {
			return err
		}
		return rateLimitFailure(route)
	}
	if !startedAt.After(now.Add(-policy.window)) {
		_, err = tx.ExecContext(ctx, `UPDATE auth_ip_rate_limits SET window_started_at=?,request_count=1,blocked_until=NULL WHERE scope_hash=?`, now, scopeHash)
		if err != nil {
			return err
		}
	} else if int(count) >= policy.limit {
		until := now.Add(policy.window)
		_, err = tx.ExecContext(ctx, `UPDATE auth_ip_rate_limits SET blocked_until=? WHERE scope_hash=?`, until, scopeHash)
		if err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		log.Printf("security_event=request_rate_limited route=%q client_ref=%x retry_after_seconds=%d", route, scopeHash[:8], int(policy.window.Seconds()))
		return rateLimitFailure(route)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE auth_ip_rate_limits SET request_count=request_count+1 WHERE scope_hash=?`, scopeHash)
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func isRetryableRateLimitLockError(err error) bool {
	var databaseError *mysql.MySQLError
	return errors.As(err, &databaseError) && (databaseError.Number == 1205 || databaseError.Number == 1213)
}

func isRateLimitRejection(err error) bool {
	var rejected failure
	return errors.As(err, &rejected) && rejected.status == http.StatusTooManyRequests
}

func rateLimitFailure(route string) error {
	if route == "api" {
		return bad(http.StatusTooManyRequests, "API_RATE_LIMITED", "请求过于频繁，请稍后再试")
	}
	return bad(http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "请求过于频繁，请稍后再试")
}

func (s *Server) maybePruneAuthRateLimits(ctx context.Context) {
	s.rateCleanupMu.Lock()
	defer s.rateCleanupMu.Unlock()
	if time.Since(s.lastRateCleanup) < time.Hour {
		return
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM auth_ip_rate_limits WHERE updated_at<DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 2 DAY) LIMIT 10000`); err == nil {
		s.lastRateCleanup = time.Now()
	}
}
