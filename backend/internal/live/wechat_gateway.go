package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type openClawWechatGateway struct {
	baseURL string
	token   string
	client  *http.Client
}

func normalizeWechatBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "https://ilinkai.weixin.qq.com", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("invalid Tencent WeChat base URL")
	}
	host := strings.ToLower(u.Hostname())
	if host != "ilinkai.weixin.qq.com" && !strings.HasSuffix(host, ".weixin.qq.com") {
		return "", errors.New("Tencent WeChat base URL host is not allowed")
	}
	return "https://" + host, nil
}

type openClawWechatLogin struct {
	SessionKey string `json:"sessionKey"`
	QRContent  string `json:"qrContent"`
	Status     string `json:"status"`
	BotToken   string `json:"botToken"`
	AccountID  string `json:"accountId"`
	UserID     string `json:"userId"`
	BaseURL    string `json:"baseUrl"`
}

func newOpenClawWechatGateway() (*openClawWechatGateway, error) {
	rawURL := strings.TrimSpace(os.Getenv("WECHAT_GATEWAY_URL"))
	token := os.Getenv("WECHAT_GATEWAY_BRIDGE_TOKEN")
	if rawURL == "" || len(token) < 32 {
		return nil, errors.New("WECHAT_GATEWAY_URL and a 32-byte WECHAT_GATEWAY_BRIDGE_TOKEN are required")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("WECHAT_GATEWAY_URL must be a plain HTTP loopback URL")
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("WECHAT_GATEWAY_URL must use a loopback IP address")
	}
	basePath := strings.TrimSuffix(u.Path, "/")
	if basePath == "" {
		basePath = "/teammind-wechat-bridge/v1"
	}
	if basePath != "/teammind-wechat-bridge/v1" {
		return nil, errors.New("WECHAT_GATEWAY_URL must use the TeamMind bridge route")
	}
	u.Path = basePath
	return &openClawWechatGateway{
		baseURL: strings.TrimSuffix(u.String(), "/"),
		token:   token,
		client:  &http.Client{Timeout: 42 * time.Second},
	}, nil
}

func (g *openClawWechatGateway) post(ctx context.Context, operation string, input, output any) error {
	allowed := map[string]bool{
		"sessions/start": true, "sessions/poll": true, "sessions/cancel": true,
		"channel/updates": true, "channel/send-text": true,
	}
	if !allowed[operation] {
		return errors.New("unsupported OpenClaw WeChat bridge operation")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/"+operation, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+g.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := g.client.Do(request)
	if err != nil {
		return fmt.Errorf("OpenClaw WeChat bridge unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenClaw WeChat bridge returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	decoder.UseNumber()
	if err = decoder.Decode(output); err != nil {
		return errors.New("OpenClaw WeChat bridge returned an invalid response")
	}
	return nil
}

func (g *openClawWechatGateway) start(ctx context.Context, sessionKey string) (openClawWechatLogin, error) {
	var result openClawWechatLogin
	err := g.post(ctx, "sessions/start", map[string]string{"sessionKey": sessionKey}, &result)
	if err == nil && (result.SessionKey != sessionKey || result.QRContent == "" || result.Status != "wait") {
		err = errors.New("OpenClaw WeChat bridge did not start a valid login")
	}
	return result, err
}

func (g *openClawWechatGateway) poll(ctx context.Context, sessionKey, verifyCode string) (openClawWechatLogin, error) {
	var result openClawWechatLogin
	input := map[string]string{"sessionKey": sessionKey}
	if verifyCode != "" {
		input["verifyCode"] = verifyCode
	}
	err := g.post(ctx, "sessions/poll", input, &result)
	return result, err
}

func (g *openClawWechatGateway) cancel(ctx context.Context, sessionKey string) {
	var result struct {
		Cancelled bool `json:"cancelled"`
	}
	_ = g.post(ctx, "sessions/cancel", map[string]string{"sessionKey": sessionKey}, &result)
}

func (g *openClawWechatGateway) getUpdates(ctx context.Context, accountID, baseURL, token, cursor string) (pluginWechatUpdates, error) {
	var result pluginWechatUpdates
	err := g.post(ctx, "channel/updates", map[string]string{
		"accountId": accountID, "baseUrl": baseURL, "token": token, "cursor": cursor,
	}, &result)
	return result, err
}

func (g *openClawWechatGateway) sendText(ctx context.Context, accountID, baseURL, token, to, contextToken, clientID, text string) error {
	var result struct {
		MessageID string `json:"messageId"`
		Ret       int    `json:"ret"`
	}
	err := g.post(ctx, "channel/send-text", map[string]string{
		"accountId": accountID, "baseUrl": baseURL, "token": token, "to": to,
		"contextToken": contextToken, "clientId": clientID, "text": text,
	}, &result)
	if err != nil {
		return err
	}
	if result.Ret != 0 {
		return fmt.Errorf("Tencent WeChat plugin send returned %d", result.Ret)
	}
	return nil
}
