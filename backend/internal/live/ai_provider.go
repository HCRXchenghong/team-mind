package live

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	aiFailureThreshold = 3
	aiCooldown         = 5 * time.Minute
)

type aiTeamContextKey struct{}

func withAITeam(ctx context.Context, teamID string) context.Context {
	if teamID == "" {
		return ctx
	}
	return context.WithValue(ctx, aiTeamContextKey{}, teamID)
}

func aiTeamFromContext(ctx context.Context) string {
	value, _ := ctx.Value(aiTeamContextKey{}).(string)
	return value
}

type aiProviderConfig struct {
	ID      string
	Name    string
	BaseURL string
	APIKey  string
	Model   string
}

type aiProviderView struct {
	ID            string     `json:"id"`
	TeamID        string     `json:"teamId"`
	TeamName      string     `json:"teamName"`
	Name          string     `json:"name"`
	BaseURL       string     `json:"baseUrl"`
	Models        []string   `json:"models"`
	Enabled       bool       `json:"enabled"`
	Priority      int        `json:"priority"`
	FailureCount  int        `json:"failureCount"`
	CooldownUntil *time.Time `json:"cooldownUntil"`
	LastError     string     `json:"lastError"`
	HasKey        bool       `json:"hasKey"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

func aiConfigKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("AI_CONFIG_ENCRYPTION_KEY"))
	if raw == "" {
		// Reuse the notification key only when an operator has not supplied a
		// dedicated AI key. This keeps existing local deployments bootable while
		// production should set AI_CONFIG_ENCRYPTION_KEY explicitly.
		raw = strings.TrimSpace(os.Getenv("NOTIFICATION_ENCRYPTION_KEY"))
	}
	key, err := decodeBase64Key(raw)
	if err != nil || len(key) != 32 {
		return nil, errors.New("AI_CONFIG_ENCRYPTION_KEY must be a base64 encoded 32 byte key")
	}
	return key, nil
}

func encryptAIKey(providerID, key string) ([]byte, error) {
	secret, err := aiConfigKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := randomBytes(gcm.NonceSize())
	if err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(key), []byte(strings.ToLower(providerID))), nil
}

func decryptAIKey(providerID string, ciphertext []byte) (string, error) {
	secret, err := aiConfigKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("invalid AI key ciphertext")
	}
	plain, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], []byte(strings.ToLower(providerID)))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func normalizeAIBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("AI 服务地址无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("AI 服务地址必须使用 http 或 https")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func validAIModels(models []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 200 || seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil, errors.New("请至少选择一个模型")
	}
	if len(out) > 50 {
		return nil, errors.New("模型数量不能超过 50 个")
	}
	return out, nil
}

func fetchAIModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	base, err := normalizeAIBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("无法连接模型服务")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型服务返回 %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, errors.New("模型列表响应格式无效")
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) != "" {
			models = append(models, item.ID)
		}
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, errors.New("模型服务没有返回可用模型")
	}
	if len(models) > 200 {
		models = models[:200]
	}
	return models, nil
}

func (s *Server) aiCandidates(ctx context.Context) ([]aiProviderConfig, error) {
	teamID := aiTeamFromContext(ctx)
	if teamID != "" && validID(teamID) {
		out, err := s.aiProviderRows(ctx, `AND team_id=UNHEX(?)`, teamID)
		if err != nil {
			return nil, err
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return s.aiProviderRows(ctx, `AND team_id IS NULL`)
}

func (s *Server) aiProviderRows(ctx context.Context, scope string, args ...any) ([]aiProviderConfig, error) {
	query := `SELECT HEX(id),name,base_url,api_key_ciphertext,models FROM ai_providers WHERE enabled=1 AND (cooldown_until IS NULL OR cooldown_until<=UTC_TIMESTAMP(3)) ` + scope + ` ORDER BY priority,id`
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []aiProviderConfig
	for rows.Next() {
		var id, name, base string
		var ciphertext, rawModels []byte
		if err = rows.Scan(&id, &name, &base, &ciphertext, &rawModels); err != nil {
			return nil, err
		}
		key, decryptErr := decryptAIKey(id, ciphertext)
		if decryptErr != nil {
			continue
		}
		var models []string
		if json.Unmarshal(rawModels, &models) != nil {
			continue
		}
		for _, model := range models {
			if strings.Contains(strings.ToLower(model), "embed") || strings.TrimSpace(model) == "" {
				continue
			}
			out = append(out, aiProviderConfig{ID: id, Name: name, BaseURL: base, APIKey: key, Model: model})
		}
	}
	return out, rows.Err()
}

func (s *Server) aiConfigured(ctx context.Context) bool {
	providers, err := s.aiCandidates(ctx)
	return err == nil && len(providers) > 0
}

func isLocalAIEndpoint(base string) bool {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func (s *Server) markAIFailure(ctx context.Context, providerID string, cause error) {
	if providerID == "" || providerID == "mock" || providerID == "env-test" {
		return
	}
	message := "model request failed"
	if cause != nil {
		message = truncateRunes(cause.Error(), 240)
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE ai_providers SET failure_count=failure_count+1,last_error=?,cooldown_until=IF(failure_count>=?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL ? SECOND),cooldown_until) WHERE id=UNHEX(?)`, message, aiFailureThreshold, int(aiCooldown.Seconds()), providerID)
}

func (s *Server) markAISuccess(ctx context.Context, providerID string) {
	if providerID == "" || providerID == "mock" || providerID == "env-test" {
		return
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE ai_providers SET failure_count=0,cooldown_until=NULL,last_error=NULL WHERE id=UNHEX(?)`, providerID)
}

// streamAIReply walks providers in priority order. Failures before the first
// emitted delta switch to the next provider; partial responses are not
// silently duplicated.
func (s *Server) streamAIReply(ctx context.Context, messages []aiMessage, emit func(string) error) (string, error) {
	providers, err := s.aiCandidates(ctx)
	if err != nil {
		return "", err
	}
	var last error
	for _, provider := range providers {
		model, wrote, err := streamAIProvider(ctx, provider, messages, emit)
		if err == nil {
			s.markAISuccess(ctx, provider.ID)
			return model, nil
		}
		s.markAIFailure(ctx, provider.ID, err)
		last = err
		if wrote {
			return "", err
		}
	}
	if last == nil {
		last = bad(http.StatusServiceUnavailable, "AI_NOT_CONFIGURED", "AI 服务尚未配置")
	}
	return "", last
}

func streamAIProvider(ctx context.Context, provider aiProviderConfig, messages []aiMessage, emit func(string) error) (string, bool, error) {
	requestPayload := map[string]any{"model": provider.Model, "messages": messages, "stream": true}
	if isLocalAIEndpoint(provider.BaseURL) {
		requestPayload["reasoning_effort"] = "none"
	}
	payload, _ := json.Marshal(requestPayload)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", false, bad(http.StatusServiceUnavailable, "AI_NOT_CONFIGURED", "AI 服务地址无效")
	}
	request.Header.Set("Authorization", "Bearer "+provider.APIKey)
	request.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 90 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", false, bad(http.StatusServiceUnavailable, "AI_UNAVAILABLE", "模型服务不可达")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", false, bad(http.StatusBadGateway, "AI_FAILED", fmt.Sprintf("模型服务返回 %d: %s", response.StatusCode, truncateRunes(strings.TrimSpace(string(body)), 160)))
	}
	wrote := false
	emitOnce := func(text string) error {
		if text == "" {
			return nil
		}
		wrote = true
		return emit(text)
	}
	if strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&result) != nil || len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
			return "", false, bad(http.StatusBadGateway, "AI_FAILED", "模型响应无效")
		}
		if err := emitOnce(result.Choices[0].Message.Content); err != nil {
			return "", wrote, err
		}
		return provider.Model, wrote, nil
	}
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 2<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	done := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", wrote, bad(http.StatusBadGateway, "AI_FAILED", "模型响应格式无效")
		}
		if len(chunk.Choices) > 0 {
			if err := emitOnce(chunk.Choices[0].Delta.Content); err != nil {
				return "", wrote, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", wrote, bad(http.StatusBadGateway, "AI_FAILED", "读取模型响应失败")
	}
	if !done {
		return "", wrote, bad(http.StatusBadGateway, "AI_FAILED", "模型连接意外结束，请重试")
	}
	if !wrote {
		return "", false, bad(http.StatusBadGateway, "AI_FAILED", "模型没有返回内容")
	}
	return provider.Model, wrote, nil
}

// aiCompletionJSON runs a non-streaming chat completion against the provider
// pool and returns the raw response body plus the selected model.
func (s *Server) aiCompletionJSON(ctx context.Context, messages []aiMessage, tools []any, maxTokens int) ([]byte, string, error) {
	providers, err := s.aiCandidates(ctx)
	if err != nil {
		return nil, "", err
	}
	var last error
	for _, provider := range providers {
		payload := map[string]any{"model": provider.Model, "messages": messages, "stream": false, "temperature": 0, "max_tokens": maxTokens}
		if isLocalAIEndpoint(provider.BaseURL) {
			payload["reasoning_effort"] = "none"
		}
		if tools != nil {
			payload["tools"] = tools
			payload["tool_choice"] = "auto"
			payload["parallel_tool_calls"] = false
		}
		body, _ := json.Marshal(payload)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, provider.BaseURL+"/chat/completions", bytes.NewReader(body))
		if reqErr != nil {
			return nil, "", bad(http.StatusServiceUnavailable, "AI_NOT_CONFIGURED", "AI 服务地址无效")
		}
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 60 * time.Second}
		resp, doErr := client.Do(req)
		if doErr != nil {
			s.markAIFailure(ctx, provider.ID, doErr)
			last = bad(http.StatusServiceUnavailable, "AI_UNAVAILABLE", "模型服务不可达")
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			log.Printf("ai provider %s model %s status=%d body=%s", provider.Name, provider.Model, resp.StatusCode, truncateRunes(string(data), 300))
			s.markAIFailure(ctx, provider.ID, errors.New("model status"))
			last = bad(http.StatusBadGateway, "AI_FAILED", fmt.Sprintf("模型服务返回 %d", resp.StatusCode))
			continue
		}
		var completion struct {
			Choices []struct {
				Message struct {
					Content   string            `json:"content"`
					ToolCalls []json.RawMessage `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(data, &completion) != nil || len(completion.Choices) != 1 ||
			(strings.TrimSpace(completion.Choices[0].Message.Content) == "" && (tools == nil || len(completion.Choices[0].Message.ToolCalls) == 0)) {
			s.markAIFailure(ctx, provider.ID, errors.New("invalid completion response"))
			last = bad(http.StatusBadGateway, "AI_FAILED", "模型响应无效")
			continue
		}
		s.markAISuccess(ctx, provider.ID)
		return data, provider.Model, nil
	}
	if last == nil {
		last = bad(http.StatusServiceUnavailable, "AI_NOT_CONFIGURED", "AI 服务尚未配置")
	}
	return nil, "", last
}

type aiAdminInput struct {
	TeamID   *string  `json:"teamId"`
	Name     string   `json:"name"`
	BaseURL  string   `json:"baseUrl"`
	APIKey   string   `json:"apiKey"`
	Models   []string `json:"models"`
	Enabled  *bool    `json:"enabled"`
	Priority *int     `json:"priority"`
}

func (s *Server) aiAdmin(w http.ResponseWriter, r *http.Request, u User, path string) error {
	if !u.SuperAdmin {
		return bad(http.StatusForbidden, "SUPER_ADMIN_REQUIRED", "需要平台超管权限")
	}
	parts := strings.Split(path, "/")
	// platform/ai/providers...
	if len(parts) >= 3 && parts[2] == "models" {
		if r.Method != http.MethodPost {
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
		var input struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			ID      string `json:"id"`
		}
		if err := body(w, r, &input); err != nil {
			return err
		}
		key := strings.TrimSpace(input.APIKey)
		if key == "" && validID(input.ID) {
			var ciphertext []byte
			if err := s.DB.QueryRowContext(r.Context(), `SELECT api_key_ciphertext FROM ai_providers WHERE id=UNHEX(?)`, input.ID).Scan(&ciphertext); err != nil {
				return err
			}
			decrypted, err := decryptAIKey(input.ID, ciphertext)
			if err != nil {
				return bad(http.StatusServiceUnavailable, "AI_KEY_UNAVAILABLE", "无法解密已保存的 API Key")
			}
			key = decrypted
		}
		models, err := fetchAIModels(r.Context(), input.BaseURL, key)
		if err != nil {
			return bad(http.StatusBadGateway, "AI_MODELS_FAILED", err.Error())
		}
		write(w, http.StatusOK, map[string]any{"models": models})
		return nil
	}
	if len(parts) == 3 && parts[2] == "providers" {
		switch r.Method {
		case http.MethodGet:
			return s.listAIProviders(w, r)
		case http.MethodPost:
			return s.createAIProvider(w, r, u)
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
	}
	if len(parts) >= 4 && parts[2] == "providers" && validID(parts[3]) {
		id := parts[3]
		if len(parts) == 5 && parts[4] == "test" && r.Method == http.MethodPost {
			var ciphertext []byte
			var baseURL string
			if err := s.DB.QueryRowContext(r.Context(), `SELECT base_url,api_key_ciphertext FROM ai_providers WHERE id=UNHEX(?)`, id).Scan(&baseURL, &ciphertext); err != nil {
				return bad(http.StatusNotFound, "NOT_FOUND", "AI 供应商不存在")
			}
			key, err := decryptAIKey(id, ciphertext)
			if err != nil {
				return bad(http.StatusServiceUnavailable, "AI_KEY_UNAVAILABLE", "无法解密已保存的 API Key")
			}
			models, err := fetchAIModels(r.Context(), baseURL, key)
			if err != nil {
				s.markAIFailure(r.Context(), id, err)
				return bad(http.StatusBadGateway, "AI_MODELS_FAILED", err.Error())
			}
			s.markAISuccess(r.Context(), id)
			write(w, http.StatusOK, map[string]any{"models": models})
			return nil
		}
		switch r.Method {
		case http.MethodPatch:
			return s.updateAIProvider(w, r, id)
		case http.MethodDelete:
			res, err := s.DB.ExecContext(r.Context(), `DELETE FROM ai_providers WHERE id=UNHEX(?)`, id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return bad(http.StatusNotFound, "NOT_FOUND", "AI 供应商不存在")
			}
			write(w, http.StatusOK, map[string]bool{"deleted": true})
			return nil
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
	}
	return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
}

func (s *Server) listAIProviders(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(p.id),p.name,p.base_url,p.models,p.enabled,p.priority,p.failure_count,p.cooldown_until,p.last_error,p.created_at,p.updated_at,COALESCE(HEX(p.team_id),''),COALESCE(t.name,'') FROM ai_providers p LEFT JOIN teams t ON t.id=p.team_id ORDER BY p.priority,p.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := make([]aiProviderView, 0)
	for rows.Next() {
		var item aiProviderView
		var rawModels []byte
		var cooldown sql.NullTime
		var lastError sql.NullString
		if err = rows.Scan(&item.ID, &item.Name, &item.BaseURL, &rawModels, &item.Enabled, &item.Priority, &item.FailureCount, &cooldown, &lastError, &item.CreatedAt, &item.UpdatedAt, &item.TeamID, &item.TeamName); err != nil {
			return err
		}
		_ = json.Unmarshal(rawModels, &item.Models)
		if cooldown.Valid {
			value := cooldown.Time
			item.CooldownUntil = &value
		}
		item.LastError = lastError.String
		item.HasKey = true
		out = append(out, item)
	}
	write(w, http.StatusOK, out)
	return rows.Err()
}

func (s *Server) createAIProvider(w http.ResponseWriter, r *http.Request, u User) error {
	var input aiAdminInput
	if err := body(w, r, &input); err != nil {
		return err
	}
	base, err := normalizeAIBaseURL(input.BaseURL)
	if err != nil {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	}
	if strings.TrimSpace(input.Name) == "" || len([]rune(input.Name)) > 120 || len(strings.TrimSpace(input.APIKey)) < 8 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请填写供应商名称和有效 API Key")
	}
	models, err := validAIModels(input.Models)
	if err != nil {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	}
	teamID := ""
	if input.TeamID != nil {
		teamID = strings.ToUpper(strings.TrimSpace(*input.TeamID))
	}
	var teamArg any
	if teamID != "" {
		if !validID(teamID) {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "团队 ID 无效")
		}
		var teams int
		if err = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM teams WHERE id=UNHEX(?) AND deleted_at IS NULL`, teamID).Scan(&teams); err != nil {
			return err
		}
		if teams == 0 {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "团队不存在")
		}
		teamArg = teamID
	}
	if _, err = aiConfigKey(); err != nil {
		return bad(http.StatusServiceUnavailable, "AI_KEY_NOT_CONFIGURED", "服务端尚未配置 AI_CONFIG_ENCRYPTION_KEY")
	}
	id := id()
	ciphertext, err := encryptAIKey(id, strings.TrimSpace(input.APIKey))
	if err != nil {
		return err
	}
	rawModels, _ := json.Marshal(models)
	enabled := true
	priority := 100
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.Priority != nil {
		priority = *input.Priority
	}
	if priority < 1 || priority > 9999 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "优先级必须在 1 到 9999 之间")
	}
	_, err = s.DB.ExecContext(r.Context(), `INSERT INTO ai_providers(id,team_id,name,base_url,api_key_ciphertext,models,enabled,priority,created_by) VALUES(UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),?,?,?,?,?,?,UNHEX(?))`, id, teamArg, teamArg, strings.TrimSpace(input.Name), base, ciphertext, rawModels, enabled, priority, u.ID)
	if err != nil {
		return err
	}
	write(w, http.StatusCreated, map[string]string{"id": id})
	return nil
}

func (s *Server) updateAIProvider(w http.ResponseWriter, r *http.Request, id string) error {
	var input aiAdminInput
	if err := body(w, r, &input); err != nil {
		return err
	}
	if input.BaseURL != "" {
		base, err := normalizeAIBaseURL(input.BaseURL)
		if err != nil {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		}
		input.BaseURL = base
	}
	if input.Name != "" && len([]rune(input.Name)) > 120 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "供应商名称过长")
	}
	if input.Priority != nil && (*input.Priority < 1 || *input.Priority > 9999) {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "优先级必须在 1 到 9999 之间")
	}
	if input.Models != nil {
		models, err := validAIModels(input.Models)
		if err != nil {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		}
		input.Models = models
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE ai_providers SET name=COALESCE(NULLIF(?,''),name),base_url=COALESCE(NULLIF(?,''),base_url),enabled=COALESCE(?,enabled),priority=COALESCE(?,priority),failure_count=0,cooldown_until=NULL,last_error=NULL WHERE id=UNHEX(?)`, strings.TrimSpace(input.Name), input.BaseURL, input.Enabled, input.Priority, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		var exists int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM ai_providers WHERE id=UNHEX(?)`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return bad(http.StatusNotFound, "NOT_FOUND", "AI 供应商不存在")
		}
	}
	if len(input.Models) > 0 {
		raw, _ := json.Marshal(input.Models)
		if _, err = tx.ExecContext(r.Context(), `UPDATE ai_providers SET models=? WHERE id=UNHEX(?)`, raw, id); err != nil {
			return err
		}
	}
	if strings.TrimSpace(input.APIKey) != "" {
		ciphertext, err := encryptAIKey(id, strings.TrimSpace(input.APIKey))
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE ai_providers SET api_key_ciphertext=? WHERE id=UNHEX(?)`, ciphertext, id); err != nil {
			return err
		}
	}
	if input.TeamID != nil {
		teamID := strings.ToUpper(strings.TrimSpace(*input.TeamID))
		if teamID != "" {
			var teams int
			if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM teams WHERE id=UNHEX(?) AND deleted_at IS NULL`, teamID).Scan(&teams); err != nil {
				return err
			}
			if teams == 0 {
				return bad(http.StatusBadRequest, "VALIDATION_ERROR", "团队不存在")
			}
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE ai_providers SET team_id=IF(?='',NULL,UNHEX(?)) WHERE id=UNHEX(?)`, teamID, teamID, id); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]bool{"updated": true})
	return nil
}
