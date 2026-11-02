package live

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type wechatSession struct {
	state                         atomic.Value
	ID, UserID, TeamID, QRContent string
	Expires                       time.Time
	Mock, Gateway                 bool
}

type wechatSessionState struct {
	Status, VerifyCode, Error string
	Done                      bool
}

func (s *wechatSession) snapshot() wechatSessionState {
	value := s.state.Load()
	if value == nil {
		return wechatSessionState{}
	}
	return *value.(*wechatSessionState)
}

// updateState publishes immutable snapshots with a CAS loop. Session status
// polling must never wait behind a QR long poll or a browser request, and the
// snapshot fields are small enough that lock-free reads are straightforward.
func (s *wechatSession) updateState(update func(*wechatSessionState)) (wechatSessionState, wechatSessionState) {
	for {
		value := s.state.Load()
		if value == nil {
			initial := &wechatSessionState{}
			if !s.state.CompareAndSwap(nil, initial) {
				continue
			}
			value = initial
		}
		previous := value.(*wechatSessionState)
		next := *previous
		update(&next)
		published := &next
		if s.state.CompareAndSwap(previous, published) {
			return *previous, next
		}
	}
}

type wechatService struct {
	mu       sync.Mutex
	sessions map[string]*wechatSession
}

func newWechatService() *wechatService { return &wechatService{sessions: map[string]*wechatSession{}} }
func wechatMode() string {
	m := strings.ToLower(strings.TrimSpace(os.Getenv("WECHAT_MODE")))
	// Mock sessions exist only for isolated automated tests. A development or
	// production process must never be able to fabricate a WeChat binding.
	if m == "mock" {
		if os.Getenv("APP_ENV") == "test" {
			return "mock"
		}
		return "unconfigured"
	}
	if m == "real" {
		return "real"
	}
	if m == "" && os.Getenv("WECHAT_GATEWAY_URL") != "" && len(wechatKey()) == 32 {
		return "real"
	}
	return "unconfigured"
}
func wechatKey() []byte {
	b, _ := base64.StdEncoding.DecodeString(os.Getenv("WECHAT_ENCRYPTION_KEY"))
	return b
}
func (s *Server) wechatRoute(w http.ResponseWriter, r *http.Request, u User, path string) error {
	p := strings.Split(path, "/")
	ws := s.wechat
	if len(p) == 3 && r.Method == "GET" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT HEX(c.id),c.provider,c.status,HEX(b.current_team_id),b.bound_at,EXISTS(SELECT 1 FROM channel_sync_states cs WHERE cs.channel_account_id=c.id AND cs.updated_at>DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 90 SECOND)),(b.sender_ciphertext IS NOT NULL AND b.context_ciphertext IS NOT NULL),(SELECT MAX(cmd.created_at) FROM channel_message_deliveries cmd WHERE cmd.channel_account_id=c.id AND cmd.user_id=b.user_id) FROM channel_bindings b JOIN channel_accounts c ON c.id=b.channel_account_id WHERE b.user_id=UNHEX(?) AND b.unbound_at IS NULL`, u.ID)
		if e != nil {
			return e
		}
		defer rows.Close()
		out := []any{}
		anyReady := false
		for rows.Next() {
			var cid, provider, status string
			var tid sql.NullString
			var at time.Time
			var live, hasContext int
			var lastInbound sql.NullTime
			if e = rows.Scan(&cid, &provider, &status, &tid, &at, &live, &hasContext, &lastInbound); e != nil {
				return e
			}
			ready := wechatMode() == "real" && live == 1 && status == "active"
			anyReady = anyReady || ready
			out = append(out, map[string]any{
				"id": cid, "provider": provider, "status": status, "teamId": tid.String, "boundAt": at,
				"messagingReady": ready, "hasContext": hasContext == 1, "lastInboundAt": nullTimeString(lastInbound),
				"notificationsReady": hasContext == 1,
			})
		}
		aiMode := "unconfigured"
		if s.aiConfigured(r.Context()) {
			aiMode = "configured"
		}
		write(w, 200, map[string]any{"bindings": out, "mode": wechatMode(), "messagingReady": anyReady, "aiMode": aiMode})
		return rows.Err()
	}
	if len(p) == 4 && p[3] == "test" && r.Method == http.MethodPost {
		reason, sendErr := s.sendWechatProactive(r.Context(), u.ID, "proactive-test-"+id(), "【TeamMind】微信主动消息测试成功，通知通道可用。")
		if sendErr != nil {
			return bad(502, "WECHAT_SEND_FAILED", "微信发送失败："+sendErr.Error())
		}
		if reason != "" {
			return bad(409, "WECHAT_CONTACT_REQUIRED", "尚未建立可复用的微信会话：请先在微信里给机器人发一条消息，再重试。")
		}
		write(w, 200, map[string]any{"sent": true})
		return nil
	}

	if len(p) == 4 && p[3] == "sessions" && r.Method == "POST" {
		var x struct {
			TeamID string `json:"teamId"`
		}
		if e := body(w, r, &x); e != nil {
			return e
		}
		if _, e := s.role(r, u, x.TeamID); e != nil {
			return e
		}
		wechatAllowed, e := s.wechatAccessAllowed(r.Context(), u.ID, x.TeamID)
		if e != nil {
			return e
		}
		if !wechatAllowed {
			return bad(http.StatusForbidden, "WECHAT_ACCESS_DENIED", "当前组织权限不允许你绑定微信")
		}
		var activeBindings int
		if e := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM channel_bindings WHERE user_id=UNHEX(?) AND unbound_at IS NULL`, u.ID).Scan(&activeBindings); e != nil {
			return e
		}
		if activeBindings > 0 {
			return bad(http.StatusConflict, "WECHAT_ALREADY_BOUND", "一个账号只能绑定一个微信，请先解绑当前微信")
		}
		mode := wechatMode()
		if mode == "unconfigured" {
			return bad(503, "WECHAT_NOT_CONFIGURED", "微信通道未配置：需要 WECHAT_MODE=real 和 32 字节加密密钥")
		}
		if mode == "real" && len(wechatKey()) != 32 {
			return bad(503, "WECHAT_KEY_REQUIRED", "微信加密密钥未配置")
		}
		session := &wechatSession{ID: id(), UserID: u.ID, TeamID: x.TeamID, Expires: time.Now().UTC().Add(5 * time.Minute), Mock: mode == "mock"}
		session.state.Store(&wechatSessionState{Status: "initializing"})
		if session.Mock {
			session.QRContent = "https://example.invalid/teammind-wechat-mock/" + session.ID
			session.updateState(func(state *wechatSessionState) { state.Status = "wait" })
		} else {
			gateway, gatewayErr := newOpenClawWechatGateway()
			if gatewayErr != nil {
				return bad(503, "WECHAT_GATEWAY_NOT_CONFIGURED", "隔离的 OpenClaw 微信 Gateway 未配置或不可用")
			}
			started, startErr := gateway.start(r.Context(), session.ID)
			if startErr != nil {
				return bad(503, "WECHAT_GATEWAY_UNAVAILABLE", "OpenClaw 微信插件暂时无法创建扫码会话")
			}
			session.QRContent = started.QRContent
			session.updateState(func(state *wechatSessionState) { state.Status = started.Status })
			session.Gateway = true
		}
		ws.mu.Lock()
		var replaced []string
		for k, v := range ws.sessions {
			if v.UserID == u.ID {
				v.updateState(func(state *wechatSessionState) {
					if !state.Done {
						state.Done = true
						state.Status = "cancelled"
					}
				})
				if v.Gateway {
					replaced = append(replaced, k)
				}
				delete(ws.sessions, k)
			}
		}
		ws.sessions[session.ID] = session
		ws.mu.Unlock()
		for _, oldSessionID := range replaced {
			go func(sid string) {
				if gateway, gatewayErr := newOpenClawWechatGateway(); gatewayErr == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					gateway.cancel(ctx, sid)
				}
			}(oldSessionID)
		}
		if session.Gateway {
			go s.pollWechatGateway(session)
		}
		write(w, 201, map[string]any{"id": session.ID, "qrContent": session.QRContent, "status": session.snapshot().Status, "expiresAt": session.Expires, "simulated": session.Mock})
		return nil
	}
	if len(p) >= 5 && p[3] == "sessions" {
		sid := p[4]
		ws.mu.Lock()
		session := ws.sessions[sid]
		ws.mu.Unlock()
		if session == nil || session.UserID != u.ID {
			return bad(404, "NOT_FOUND", "扫码会话不存在")
		}
		state := session.snapshot()
		if time.Now().UTC().After(session.Expires) && !state.Done {
			previous, updated := session.updateState(func(state *wechatSessionState) {
				if !state.Done {
					state.Status = "expired"
					state.Done = true
				}
			})
			state = updated
			if !previous.Done && updated.Status == "expired" && session.Gateway {
				go func(sid string) {
					if gateway, gatewayErr := newOpenClawWechatGateway(); gatewayErr == nil {
						ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
						defer cancel()
						gateway.cancel(ctx, sid)
					}
				}(sid)
			}
		}
		if len(p) == 5 && r.Method == "GET" {
			write(w, 200, map[string]any{"id": session.ID, "status": state.Status, "error": state.Error, "expiresAt": session.Expires, "qrContent": session.QRContent, "simulated": session.Mock})
			return nil
		}
		if len(p) == 5 && r.Method == "DELETE" {
			_, updated := session.updateState(func(state *wechatSessionState) {
				if !state.Done {
					state.Done = true
					state.Status = "cancelled"
				}
			})
			gatewaySession := session.Gateway && updated.Status == "cancelled"
			if gatewaySession {
				go func() {
					if gateway, gatewayErr := newOpenClawWechatGateway(); gatewayErr == nil {
						ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
						defer cancel()
						gateway.cancel(ctx, sid)
					}
				}()
			}
			write(w, 200, map[string]bool{"cancelled": updated.Status == "cancelled"})
			return nil
		}
		if len(p) == 6 && p[5] == "verify" && r.Method == "POST" {
			var x struct {
				Code string `json:"code"`
			}
			if e := body(w, r, &x); e != nil {
				return e
			}
			if len(x.Code) < 4 || len(x.Code) > 12 {
				return bad(400, "VALIDATION_ERROR", "验证码格式无效")
			}
			previous, updated := session.updateState(func(state *wechatSessionState) {
				if !state.Done {
					state.VerifyCode = x.Code
				}
			})
			if previous.Done || updated.Done {
				return bad(http.StatusConflict, "SESSION_FINALIZED", "扫码会话已结束，请重新连接")
			}
			write(w, 200, map[string]bool{"submitted": true})
			return nil
		}
		if len(p) == 6 && p[5] == "mock-confirm" && r.Method == "POST" && session.Mock && os.Getenv("APP_ENV") == "test" {
			previous, _ := session.updateState(func(state *wechatSessionState) {
				if !state.Done {
					state.Status = "confirmed_simulated"
					state.Done = true
				}
			})
			if previous.Done {
				return bad(http.StatusConflict, "SESSION_FINALIZED", "扫码会话已结束，请重新连接")
			}
			write(w, 200, map[string]bool{"simulated": true})
			return nil
		}
	}
	if len(p) == 4 && r.Method == "DELETE" && validID(p[3]) {
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		res, e := tx.ExecContext(r.Context(), `UPDATE channel_bindings SET unbound_at=UTC_TIMESTAMP(3) WHERE channel_account_id=UNHEX(?) AND user_id=UNHEX(?) AND unbound_at IS NULL`, p[3], u.ID)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return bad(404, "NOT_FOUND", "绑定不存在")
		}
		if e = addAudit(tx, u.ID, "", "wechat.unbind", "channel", p[3]); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		write(w, 200, map[string]bool{"unbound": true})
		return nil
	}
	return bad(404, "NOT_FOUND", "资源不存在")
}
func (s *Server) pollWechatGateway(session *wechatSession) {
	gateway, err := newOpenClawWechatGateway()
	if err != nil {
		session.updateState(func(state *wechatSessionState) {
			state.Status = "failed"
			state.Error = "OpenClaw 微信 Gateway 配置已失效"
			state.Done = true
		})
		return
	}
	for {
		state := session.snapshot()
		if state.Done {
			return
		}
		if time.Now().UTC().After(session.Expires) {
			_, updated := session.updateState(func(state *wechatSessionState) {
				if !state.Done {
					state.Status = "expired"
					state.Done = true
				}
			})
			if updated.Status != "expired" {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			gateway.cancel(ctx, session.ID)
			cancel()
			return
		}
		verifyCode := state.VerifyCode

		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		result, pollErr := gateway.poll(ctx, session.ID, verifyCode)
		cancel()
		if pollErr != nil {
			previous, _ := session.updateState(func(state *wechatSessionState) {
				if !state.Done {
					state.Status = "failed"
					state.Error = "微信渠道连接失败，请检查 Gateway 网络后重新扫码"
					state.Done = true
				}
			})
			if previous.Done {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			gateway.cancel(ctx, session.ID)
			cancel()
			return
		}

		previous, _ := session.updateState(func(state *wechatSessionState) {
			if state.Done {
				return
			}
			if result.Status != "" {
				state.Status = result.Status
			}
			if result.Status == "scaned" || result.Status == "verify_code_blocked" {
				state.VerifyCode = ""
			}
			if result.Status == "confirmed" {
				// Keep the public status short of "confirmed" until the encrypted
				// identity mapping and audit record commit successfully.
				state.Status = "confirming"
				state.Done = true
			}
		})
		if previous.Done {
			return
		}
		switch result.Status {
		case "confirmed":
			if result.AccountID == "" || result.UserID == "" || result.BotToken == "" {
				session.updateState(func(state *wechatSessionState) {
					state.Status = "failed"
					state.Error = "渠道确认信息不完整"
				})
				return
			}
			if err = s.storeWechatBindingWithBaseURL(session, result.AccountID, result.UserID, result.BotToken, result.BaseURL); err != nil {
				log.Printf("wechat binding failed user=%s team=%s: %v", session.UserID, session.TeamID, err)
				message := "微信绑定保存失败，请重试"
				switch {
				case strings.Contains(err.Error(), "account claimed"):
					message = "该微信已绑定其他账号"
				case strings.Contains(err.Error(), "一个账号只能"):
					message = "当前账号已绑定其他微信"
				case strings.Contains(err.Error(), "binding user or team"):
					message = "当前团队不可用，请刷新后重试"
				}
				session.updateState(func(state *wechatSessionState) {
					state.Status = "failed"
					state.Error = message
				})
				return
			}
			session.updateState(func(state *wechatSessionState) {
				state.Status = "confirmed"
				state.Error = ""
			})
			return
		case "expired", "already_bound", "failed", "missing":
			session.updateState(func(state *wechatSessionState) {
				if result.Status == "already_bound" {
					state.Status = "failed"
					state.Error = "该微信通道已绑定到另一个 OpenClaw 实例，不能关联到 TeamMind"
				} else if result.Status != "expired" {
					state.Status = "failed"
					state.Error = "微信渠道登录失败，请重新发起扫码"
				}
				state.Done = true
			})
			return
		default:
			time.Sleep(time.Second)
		}
	}
}

func encryptWechat(raw string) ([]byte, error) {
	block, e := aes.NewCipher(wechatKey())
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return g.Seal(nonce, nonce, []byte(raw), nil), nil
}
func (s *Server) storeWechatBinding(session *wechatSession, botID, scannerID, secret string) error {
	return s.storeWechatBindingWithBaseURL(session, botID, scannerID, secret, "https://ilinkai.weixin.qq.com")
}

func (s *Server) storeWechatBindingWithBaseURL(session *wechatSession, botID, scannerID, secret, baseURL string) error {
	baseURL, e := normalizeWechatBaseURL(baseURL)
	if e != nil {
		return e
	}
	credential, e := json.Marshal(map[string]string{"token": secret, "baseUrl": baseURL})
	if e != nil {
		return e
	}
	ciphertext, e := encryptWechat(string(credential))
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var active int
	e = tx.QueryRow(`SELECT 1 FROM team_memberships m JOIN users u ON u.id=m.user_id JOIN teams t ON t.id=m.team_id WHERE m.team_id=UNHEX(?) AND m.user_id=UNHEX(?) AND m.status='active' AND u.status='active' AND t.status='active' FOR UPDATE`, session.TeamID, session.UserID).Scan(&active)
	if e == sql.ErrNoRows {
		return errors.New("binding user or team is no longer active")
	}
	if e != nil {
		return e
	}
	var cid string
	e = tx.QueryRow(`SELECT HEX(id) FROM channel_accounts WHERE provider='openclaw-weixin' AND provider_user_hash=? FOR UPDATE`, hash(botID)).Scan(&cid)
	if e == sql.ErrNoRows {
		cid = id()
		_, e = tx.Exec(`INSERT INTO channel_accounts(id,provider,provider_user_hash,scanner_hash,provider_base_url,encrypted_credentials,status) VALUES(UNHEX(?),'openclaw-weixin',?,?,?,?,'active')`, cid, hash(botID), hash(scannerID), baseURL, ciphertext)
	} else if e == nil {
		var owner string
		e = tx.QueryRow(`SELECT HEX(user_id) FROM channel_bindings WHERE channel_account_id=UNHEX(?) AND unbound_at IS NULL FOR UPDATE`, cid).Scan(&owner)
		if e == nil && owner != session.UserID {
			return errors.New("account claimed")
		}
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		_, e = tx.Exec(`UPDATE channel_accounts SET scanner_hash=?,provider_base_url=?,encrypted_credentials=?,status='active' WHERE id=UNHEX(?)`, hash(scannerID), baseURL, ciphertext, cid)
	}
	if e != nil {
		return e
	}
	var otherBinding string
	e = tx.QueryRow(`SELECT HEX(channel_account_id) FROM channel_bindings WHERE user_id=UNHEX(?) AND unbound_at IS NULL AND channel_account_id<>UNHEX(?) FOR UPDATE`, session.UserID, cid).Scan(&otherBinding)
	if e == nil {
		return errors.New("一个账号只能绑定一个微信，请先解绑当前微信")
	}
	if e != sql.ErrNoRows {
		return e
	}
	_, e = tx.Exec(`INSERT INTO channel_bindings(channel_account_id,user_id,current_team_id) VALUES(UNHEX(?),UNHEX(?),UNHEX(?)) ON DUPLICATE KEY UPDATE unbound_at=NULL,current_team_id=VALUES(current_team_id),bound_at=UTC_TIMESTAMP(3)`, cid, session.UserID, session.TeamID)
	if e != nil {
		return e
	}
	if e = addAudit(tx, session.UserID, session.TeamID, "wechat.bind", "channel", cid); e != nil {
		return e
	}
	return tx.Commit()
}
