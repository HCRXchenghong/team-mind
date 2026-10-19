package live

// This worker only calls TeamMind's authenticated loopback bridge, which in
// turn invokes the pinned @tencent-weixin/openclaw-weixin plugin API. The Go
// structs below decode the plugin-normalized message envelope; Go never sends
// requests to Tencent's iLink endpoints directly.
import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type pluginWechatMessage struct {
	MessageID   json.Number `json:"message_id"`
	From        string      `json:"from_user_id"`
	GroupID     string      `json:"group_id"`
	MessageType int         `json:"message_type"`
	Context     string      `json:"context_token"`
	Items       []struct {
		Type int `json:"type"`
		Text struct {
			Text string `json:"text"`
		} `json:"text_item"`
	} `json:"item_list"`
}
type pluginWechatUpdates struct {
	Ret      int                   `json:"ret"`
	ErrCode  int                   `json:"errcode"`
	Messages []pluginWechatMessage `json:"msgs"`
	Buf      string                `json:"get_updates_buf"`
}

func decryptWechat(raw []byte) (string, error) {
	block, err := aes.NewCipher(wechatKey())
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < g.NonceSize() {
		return "", errors.New("invalid channel ciphertext")
	}
	plain, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	return string(plain), err
}

type wechatCredentials struct {
	Token   string `json:"token"`
	BaseURL string `json:"baseUrl"`
}

func decodeWechatCredentials(raw, baseURL string) (wechatCredentials, error) {
	var credentials wechatCredentials
	if json.Unmarshal([]byte(raw), &credentials) == nil && credentials.Token != "" {
		if credentials.BaseURL == "" {
			credentials.BaseURL = baseURL
		}
	} else {
		// Read credentials written by earlier TeamMind versions while relying
		// on the migration's provider_base_url default.
		credentials = wechatCredentials{Token: raw, BaseURL: baseURL}
	}
	normalized, err := normalizeWechatBaseURL(credentials.BaseURL)
	if err != nil || credentials.Token == "" {
		return wechatCredentials{}, errors.New("invalid encrypted WeChat credentials")
	}
	credentials.BaseURL = normalized
	return credentials, nil
}

func (s *Server) RunWechatWorker(ctx context.Context) error {
	if wechatMode() != "real" || len(wechatKey()) != 32 {
		return errors.New("wechat-worker requires WECHAT_MODE=real and WECHAT_ENCRYPTION_KEY")
	}
	if _, err := newOpenClawWechatGateway(); err != nil {
		return errors.New("wechat-worker requires the authenticated OpenClaw Tencent WeChat bridge")
	}
	var wg sync.WaitGroup
	running := map[string]context.CancelFunc{}
	finished := make(chan string, 100)
	defer func() {
		for _, cancel := range running {
			cancel()
		}
		wg.Wait()
	}()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case cid := <-finished:
			if cancel := running[cid]; cancel != nil {
				cancel()
				delete(running, cid)
			}
		default:
		}
		rows, err := s.DB.QueryContext(ctx, `SELECT HEX(c.id) FROM channel_accounts c JOIN channel_bindings b ON b.channel_account_id=c.id JOIN users u ON u.id=b.user_id JOIN team_memberships m ON m.user_id=u.id AND m.team_id=b.current_team_id JOIN teams t ON t.id=m.team_id WHERE c.status='active' AND b.unbound_at IS NULL AND u.status='active' AND m.status='active' AND t.status='active'`)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		active := map[string]bool{}
		for rows.Next() {
			var cid string
			if err = rows.Scan(&cid); err != nil {
				break
			}
			active[cid] = true
		}
		rows.Close()
		if err != nil {
			return err
		}
		for cid, cancel := range running {
			if !active[cid] {
				cancel()
			}
		}
		for cid := range active {
			if running[cid] != nil {
				continue
			}
			accountCtx, cancel := context.WithCancel(ctx)
			running[cid] = cancel
			wg.Add(1)
			go func(account string) {
				defer wg.Done()
				s.pollChannel(accountCtx, account)
				select {
				case finished <- account:
				case <-ctx.Done():
				}
			}(cid)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Server) pollChannel(ctx context.Context, account string) {
	lease, err := s.DB.Conn(ctx)
	if err != nil {
		return
	}
	defer lease.Close()
	lockName := "tm-wechat-" + account
	var acquired int
	if err = lease.QueryRowContext(ctx, `SELECT GET_LOCK(?,0)`, lockName).Scan(&acquired); err != nil || acquired != 1 {
		return
	}
	defer func() {
		var released sql.NullInt64
		_ = lease.QueryRowContext(context.Background(), `SELECT RELEASE_LOCK(?)`, lockName).Scan(&released)
	}()
	var ciphertext []byte
	var providerBaseURL string
	if err := s.DB.QueryRowContext(ctx, `SELECT encrypted_credentials,provider_base_url FROM channel_accounts WHERE id=UNHEX(?) AND status='active'`, account).Scan(&ciphertext, &providerBaseURL); err != nil {
		return
	}
	decrypted, err := decryptWechat(ciphertext)
	if err != nil {
		log.Printf("wechat worker: cannot decrypt account %s", account)
		return
	}
	credentials, err := decodeWechatCredentials(decrypted, providerBaseURL)
	if err != nil {
		log.Printf("wechat worker: invalid credentials for account %s", account)
		return
	}
	gateway, err := newOpenClawWechatGateway()
	if err != nil {
		log.Printf("wechat worker: Tencent plugin bridge unavailable for account %s", account)
		return
	}
	var cursor string
	err = s.DB.QueryRowContext(ctx, `SELECT updates_buf FROM channel_sync_states WHERE channel_account_id=UNHEX(?)`, account).Scan(&cursor)
	if err != nil && err != sql.ErrNoRows {
		return
	}
	for ctx.Err() == nil {
		if err := s.retryDeliveries(ctx, gateway, account, credentials); err != nil {
			log.Printf("wechat worker retry account %s: %v", account, err)
		}
		pollCtx, cancelPoll := context.WithTimeout(ctx, 40*time.Second)
		reply, err := gateway.getUpdates(pollCtx, account, credentials.BaseURL, credentials.Token, cursor)
		cancelPoll()
		if err != nil || reply.Ret != 0 || reply.ErrCode != 0 {
			if ctx.Err() != nil {
				return
			}
			log.Printf("wechat worker poll account %s failed (ret=%d code=%d)", account, reply.Ret, reply.ErrCode)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		// Persist inbound records and cursor atomically. Unknown senders are
		// recorded as rejected, never routed into an AI conversation.
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			continue
		}
		for _, message := range reply.Messages {
			if err = s.recordChannelMessage(ctx, tx, account, message); err != nil {
				break
			}
		}
		if err == nil {
			nextCursor := cursor
			if reply.Buf != "" {
				nextCursor = reply.Buf
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO channel_sync_states(channel_account_id,updates_buf) VALUES(UNHEX(?),?) ON DUPLICATE KEY UPDATE updates_buf=VALUES(updates_buf),updated_at=UTC_TIMESTAMP(3)`, account, nextCursor)
		}
		if err != nil {
			tx.Rollback()
			log.Printf("wechat worker persist account %s: %v", account, err)
			continue
		}
		if err = tx.Commit(); err != nil {
			continue
		}
		if reply.Buf != "" {
			cursor = reply.Buf
		}
	}
}

func inboundText(message pluginWechatMessage) string {
	var parts []string
	for _, item := range message.Items {
		if item.Type == 1 && item.Text.Text != "" {
			parts = append(parts, item.Text.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func configuredWechatAIProvider() (string, error) {
	provider := strings.TrimSpace(os.Getenv("AI_PROVIDER"))
	if provider == "" || provider == "mock" {
		return "", errors.New("real AI provider is not configured; refusing to send a simulated WeChat reply")
	}
	if provider != "openai_compatible" || strings.TrimSpace(os.Getenv("AI_BASE_URL")) == "" || strings.TrimSpace(os.Getenv("AI_MODEL")) == "" || strings.TrimSpace(os.Getenv("AI_API_KEY")) == "" {
		return "", errors.New("real AI provider configuration is incomplete; refusing to send a WeChat reply")
	}
	return provider, nil
}

func (s *Server) recordChannelMessage(ctx context.Context, tx *sql.Tx, account string, message pluginWechatMessage) error {
	messageID := message.MessageID.String()
	if messageID == "" || len(messageID) > 80 || message.From == "" {
		return nil
	}
	var already int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?) AND provider_message_id=?`, account, messageID).Scan(&already)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	var userID, teamID string
	err = tx.QueryRowContext(ctx, `SELECT HEX(b.user_id),HEX(b.current_team_id) FROM channel_accounts c JOIN channel_bindings b ON b.channel_account_id=c.id JOIN users u ON u.id=b.user_id JOIN team_memberships m ON m.team_id=b.current_team_id AND m.user_id=u.id JOIN teams t ON t.id=m.team_id WHERE c.id=UNHEX(?) AND c.status='active' AND c.scanner_hash=? AND b.unbound_at IS NULL AND u.status='active' AND m.status='active' AND t.status='active'`, account, hash(message.From)).Scan(&userID, &teamID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows || message.MessageType != 1 || message.GroupID != "" { // no pairing or group-chat fallback
		_, err = tx.ExecContext(ctx, `INSERT INTO channel_message_deliveries(channel_account_id,provider_message_id,sender_hash,status,last_error) VALUES(UNHEX(?),?,?,'rejected',?)`, account, messageID, hash(message.From), "unbound sender or unsupported message type")
		return err
	}
	text := inboundText(message)
	if text == "" || len(text) > 12000 {
		_, err = tx.ExecContext(ctx, `INSERT INTO channel_message_deliveries(channel_account_id,provider_message_id,sender_hash,user_id,team_id,status,last_error) VALUES(UNHEX(?),?,?,UNHEX(?),UNHEX(?),'rejected',?)`, account, messageID, hash(message.From), userID, teamID, "unsupported media or length")
		return err
	}
	var cid string
	err = tx.QueryRowContext(ctx, `SELECT HEX(conversation_id) FROM channel_conversations WHERE channel_account_id=UNHEX(?) AND sender_hash=? AND user_id=UNHEX(?) AND team_id=UNHEX(?)`, account, hash(message.From), userID, teamID).Scan(&cid)
	if err == sql.ErrNoRows {
		cid = id()
		if _, err = tx.ExecContext(ctx, `INSERT INTO conversations(id,owner_user_id,team_id,visibility,title) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),'team_context',?)`, cid, userID, teamID, "微信私聊"); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO channel_conversations(channel_account_id,sender_hash,user_id,team_id,conversation_id) VALUES(UNHEX(?),?,UNHEX(?),UNHEX(?),UNHEX(?))`, account, hash(message.From), userID, teamID, cid)
	}
	if err != nil {
		return err
	}
	messageRow := id()
	_, err = tx.ExecContext(ctx, `INSERT INTO conversation_messages(id,conversation_id,role,content) VALUES(UNHEX(?),UNHEX(?),'user',?)`, messageRow, cid, text)
	if err != nil {
		return err
	}
	sender, err := encryptWechat(message.From)
	if err != nil {
		return err
	}
	contextToken, err := encryptWechat(message.Context)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_message_deliveries(channel_account_id,provider_message_id,sender_hash,user_id,team_id,conversation_id,inbound_message_id,sender_ciphertext,context_ciphertext,next_retry_at) VALUES(UNHEX(?),?,?,UNHEX(?),UNHEX(?),UNHEX(?),UNHEX(?),?,?,UTC_TIMESTAMP(3))`, account, messageID, hash(message.From), userID, teamID, cid, messageRow, sender, contextToken)
	return err
}

func (s *Server) retryDeliveries(ctx context.Context, gateway *openClawWechatGateway, account string, credentials wechatCredentials) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT provider_message_id,HEX(user_id),HEX(team_id),HEX(conversation_id),HEX(inbound_message_id),sender_ciphertext,context_ciphertext,reply_ciphertext,attempts FROM channel_message_deliveries WHERE channel_account_id=UNHEX(?) AND status IN ('pending','failed') AND (next_retry_at IS NULL OR next_retry_at<=UTC_TIMESTAMP(3)) ORDER BY created_at,provider_message_id LIMIT 10`, account)
	if err != nil {
		return err
	}
	type delivery struct {
		mid, user, team, conv, inbound string
		sender, context, reply         []byte
		attempts                       int
	}
	var pending []delivery
	for rows.Next() {
		var d delivery
		if err = rows.Scan(&d.mid, &d.user, &d.team, &d.conv, &d.inbound, &d.sender, &d.context, &d.reply, &d.attempts); err != nil {
			break
		}
		pending = append(pending, d)
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range pending {
		if err := s.deliverChannelReply(ctx, gateway, account, credentials, d.mid, d.user, d.team, d.conv, d.inbound, d.sender, d.context, d.reply, d.attempts); err != nil {
			log.Printf("wechat delivery account %s failed: %v", account, err)
		}
	}
	return nil
}

func (s *Server) deliverChannelReply(ctx context.Context, gateway *openClawWechatGateway, account string, credentials wechatCredentials, mid, user, team, conv, inbound string, encryptedSender, encryptedContext, encryptedReply []byte, attempts int) error {
	// Check every time, including retries after a process restart or membership change.
	var allowed int
	err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM channel_accounts c JOIN channel_bindings b ON b.channel_account_id=c.id JOIN users u ON u.id=b.user_id JOIN team_memberships m ON m.user_id=u.id AND m.team_id=b.current_team_id JOIN teams t ON t.id=m.team_id WHERE c.id=UNHEX(?) AND c.status='active' AND b.user_id=UNHEX(?) AND b.current_team_id=UNHEX(?) AND b.unbound_at IS NULL AND u.status='active' AND m.status='active' AND t.status='active'`, account, user, team).Scan(&allowed)
	if err == sql.ErrNoRows {
		_, err = s.DB.ExecContext(ctx, `UPDATE channel_message_deliveries SET status='rejected',last_error='binding or membership revoked' WHERE channel_account_id=UNHEX(?) AND provider_message_id=?`, account, mid)
		return err
	}
	if err != nil {
		return err
	}
	sender, err := decryptWechat(encryptedSender)
	if err != nil {
		return err
	}
	contextToken, err := decryptWechat(encryptedContext)
	if err != nil {
		return err
	}
	var scanner []byte
	if err = s.DB.QueryRowContext(ctx, `SELECT scanner_hash FROM channel_accounts WHERE id=UNHEX(?)`, account).Scan(&scanner); err != nil {
		return err
	}
	if !bytes.Equal(scanner, hash(sender)) {
		return errors.New("sender no longer matches binding")
	}
	var reply string
	if len(encryptedReply) > 0 {
		reply, err = decryptWechat(encryptedReply)
		if err != nil {
			return err
		}
	} else {
		var prompt string
		if err = s.DB.QueryRowContext(ctx, `SELECT content FROM conversation_messages WHERE id=UNHEX(?) AND conversation_id=UNHEX(?) AND role='user'`, inbound, conv).Scan(&prompt); err != nil {
			return err
		}
		provider, providerErr := configuredWechatAIProvider()
		if providerErr != nil {
			return s.deferChannelDelivery(ctx, account, mid, attempts, providerErr)
		}
		messages, contextErr := s.recentAIMessages(ctx, conv, sql.NullString{String: team, Valid: true}, user, prompt, inbound)
		if contextErr != nil {
			return s.deferChannelDelivery(ctx, account, mid, attempts, contextErr)
		}
		var model string
		reply, model, err = replyMessages(ctx, provider, messages)
		if err != nil {
			return s.deferChannelDelivery(ctx, account, mid, attempts, err)
		}
		ciphertext, e := encryptWechat(reply)
		if e != nil {
			return e
		}
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, `INSERT INTO conversation_messages(id,conversation_id,role,content,provider,model) VALUES(UNHEX(?),UNHEX(?),'assistant',?,?,?)`, id(), conv, reply, provider, model); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE channel_message_deliveries SET reply_ciphertext=?,status='pending' WHERE channel_account_id=UNHEX(?) AND provider_message_id=?`, ciphertext, account, mid); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	clientID := "teammind-" + account + "-" + mid
	err = gateway.sendText(ctx, account, credentials.BaseURL, credentials.Token, sender, contextToken, clientID, reply)
	if err != nil {
		return s.deferChannelDelivery(ctx, account, mid, attempts, err)
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE channel_message_deliveries SET status='sent',sent_at=UTC_TIMESTAMP(3),attempts=attempts+1,last_error=NULL,reply_ciphertext=NULL,sender_ciphertext=NULL,context_ciphertext=NULL WHERE channel_account_id=UNHEX(?) AND provider_message_id=?`, account, mid)
	return err
}

func (s *Server) deferChannelDelivery(ctx context.Context, account, mid string, attempts int, cause error) error {
	// Retry on a bounded schedule. Never log token, sender, message, or reply.
	delay := 1 << uint(minInt(attempts, 8))
	if delay > 300 {
		delay = 300
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE channel_message_deliveries SET status='failed',attempts=attempts+1,next_retry_at=DATE_ADD(UTC_TIMESTAMP(3),INTERVAL ? SECOND),last_error=? WHERE channel_account_id=UNHEX(?) AND provider_message_id=?`, delay, "delivery failed", account, mid)
	if err != nil {
		return err
	}
	return cause
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
