package live

import (
	"context"
	"database/sql"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type platformRuntimeConfig struct {
	Configured       bool
	Registration     bool
	PublicURL        string
	SMTPHost         string
	SMTPPort         string
	SMTPUser         string
	SMTPPassword     []byte
	SMTPFrom         string
	SMTPTLSInsecure  bool
	STTProvider      string
	STTBaseURL       string
	STTAPIKey        []byte
	STTModel         string
	STTLanguage      string
	VAPIDSubject     string
	WeChatMode       string
	WeChatGatewayURL string
	WeChatToken      []byte
}

func (s *Server) loadPlatformRuntimeConfig(ctx context.Context) (platformRuntimeConfig, error) {
	var c platformRuntimeConfig
	var publicURL, smtpHost, smtpPort, smtpUser, smtpFrom, sttProvider, sttBase, sttModel, sttLang, vapidSubject, wechatMode, gatewayURL sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT updated_by IS NOT NULL,registration_enabled,public_url,smtp_host,smtp_port,smtp_user,smtp_password_ciphertext,smtp_from,smtp_tls_insecure,stt_provider,stt_base_url,stt_api_key_ciphertext,stt_model,stt_language,vapid_subject,wechat_mode,wechat_gateway_url,wechat_gateway_bridge_token_ciphertext FROM platform_settings WHERE id=1`).Scan(
		&c.Configured, &c.Registration, &publicURL, &smtpHost, &smtpPort, &smtpUser, &c.SMTPPassword, &smtpFrom, &c.SMTPTLSInsecure,
		&sttProvider, &sttBase, &c.STTAPIKey, &sttModel, &sttLang, &vapidSubject, &wechatMode, &gatewayURL, &c.WeChatToken,
	)
	if err != nil {
		return c, err
	}
	c.PublicURL, c.SMTPHost, c.SMTPPort = publicURL.String, smtpHost.String, smtpPort.String
	c.SMTPUser, c.SMTPFrom = smtpUser.String, smtpFrom.String
	c.STTProvider, c.STTBaseURL, c.STTModel, c.STTLanguage = sttProvider.String, sttBase.String, sttModel.String, sttLang.String
	c.VAPIDSubject, c.WeChatMode, c.WeChatGatewayURL = vapidSubject.String, wechatMode.String, gatewayURL.String
	return c, nil
}

func setRuntimeEnv(key, value string, _ bool) {
	if strings.TrimSpace(value) != "" {
		_ = os.Setenv(key, value)
	}
}

func decryptPlatformSecret(aad string, ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	return decryptAIKey(aad, ciphertext)
}

// applyPlatformRuntimeConfig projects encrypted admin settings into the
// process environment. Existing env values remain bootstrap fallbacks when a
// field has never been saved from the console.
func (s *Server) applyPlatformRuntimeConfig(ctx context.Context) error {
	cfg, err := s.loadPlatformRuntimeConfig(ctx)
	if err != nil {
		return err
	}
	setRuntimeEnv("PUBLIC_URL", cfg.PublicURL, cfg.Configured)
	setRuntimeEnv("SMTP_HOST", cfg.SMTPHost, cfg.Configured)
	setRuntimeEnv("SMTP_PORT", cfg.SMTPPort, cfg.Configured)
	setRuntimeEnv("SMTP_USER", cfg.SMTPUser, cfg.Configured)
	setRuntimeEnv("SMTP_FROM", cfg.SMTPFrom, cfg.Configured)
	_ = os.Setenv("SMTP_TLS_INSECURE", strconv.FormatBool(cfg.SMTPTLSInsecure))
	if password, err := decryptPlatformSecret("platform:smtp_password", cfg.SMTPPassword); err == nil {
		setRuntimeEnv("SMTP_PASSWORD", password, cfg.Configured)
	}
	setRuntimeEnv("STT_PROVIDER", cfg.STTProvider, cfg.Configured)
	setRuntimeEnv("STT_BASE_URL", cfg.STTBaseURL, cfg.Configured)
	setRuntimeEnv("STT_MODEL", cfg.STTModel, cfg.Configured)
	setRuntimeEnv("STT_LANGUAGE", cfg.STTLanguage, cfg.Configured)
	if key, err := decryptPlatformSecret("platform:stt_api_key", cfg.STTAPIKey); err == nil {
		setRuntimeEnv("STT_API_KEY", key, cfg.Configured)
	}
	setRuntimeEnv("VAPID_SUBJECT", cfg.VAPIDSubject, cfg.Configured)
	setRuntimeEnv("WECHAT_MODE", cfg.WeChatMode, cfg.Configured)
	setRuntimeEnv("WECHAT_GATEWAY_URL", cfg.WeChatGatewayURL, cfg.Configured)
	if token, err := decryptPlatformSecret("platform:wechat_bridge_token", cfg.WeChatToken); err == nil {
		setRuntimeEnv("WECHAT_GATEWAY_BRIDGE_TOKEN", token, cfg.Configured)
	}
	return nil
}

func validatePublicURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return bad(400, "VALIDATION_ERROR", "Public URL 必须是 http(s) 地址")
	}
	return nil
}

func validateWechatGatewayURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return bad(400, "VALIDATION_ERROR", "微信网关必须是 loopback HTTP 地址")
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return bad(400, "VALIDATION_ERROR", "微信网关必须使用 loopback 地址")
	}
	if strings.TrimSuffix(u.Path, "/") != "/teammind-wechat-bridge/v1" {
		return bad(400, "VALIDATION_ERROR", "微信网关路径必须是 /teammind-wechat-bridge/v1")
	}
	return nil
}

func platformSettingsView(cfg platformRuntimeConfig) map[string]any {
	if cfg.SMTPPort == "" {
		cfg.SMTPPort = "465"
	}
	if cfg.WeChatMode == "" {
		cfg.WeChatMode = wechatMode()
	}
	return map[string]any{
		"registrationEnabled": cfg.Registration,
		"publicUrl":           cfg.PublicURL,
		"smtpHost":            cfg.SMTPHost,
		"smtpPort":            cfg.SMTPPort,
		"smtpUser":            cfg.SMTPUser,
		"smtpFrom":            cfg.SMTPFrom,
		"smtpTlsInsecure":     cfg.SMTPTLSInsecure,
		"smtpPasswordSet":     len(cfg.SMTPPassword) > 0,
		"sttProvider":         cfg.STTProvider,
		"sttBaseUrl":          cfg.STTBaseURL,
		"sttModel":            cfg.STTModel,
		"sttLanguage":         cfg.STTLanguage,
		"sttApiKeySet":        len(cfg.STTAPIKey) > 0,
		"vapidSubject":        cfg.VAPIDSubject,
		"wechatMode":          cfg.WeChatMode,
		"wechatGatewayUrl":    cfg.WeChatGatewayURL,
		"wechatTokenSet":      len(cfg.WeChatToken) > 0,
	}
}

// platformSettings reads and updates runtime configuration. Secrets are
// encrypted at rest and never returned; only a boolean "set" flag is exposed.
func (s *Server) platformSettings(w http.ResponseWriter, r *http.Request, u User) error {
	if r.Method == "GET" {
		cfg, err := s.loadPlatformRuntimeConfig(r.Context())
		if err != nil {
			return err
		}
		write(w, 200, platformSettingsView(cfg))
		return nil
	}
	if r.Method != "PATCH" {
		return bad(405, "METHOD_NOT_ALLOWED", "不支持此请求方式")
	}
	var input struct {
		RegistrationEnabled *bool   `json:"registrationEnabled"`
		PublicURL           *string `json:"publicUrl"`
		SMTPHost            *string `json:"smtpHost"`
		SMTPPort            *string `json:"smtpPort"`
		SMTPUser            *string `json:"smtpUser"`
		SMTPPassword        *string `json:"smtpPassword"`
		SMTPFrom            *string `json:"smtpFrom"`
		SMTPTLSInsecure     *bool   `json:"smtpTlsInsecure"`
		STTProvider         *string `json:"sttProvider"`
		STTBaseURL          *string `json:"sttBaseUrl"`
		STTAPIKey           *string `json:"sttApiKey"`
		STTModel            *string `json:"sttModel"`
		STTLanguage         *string `json:"sttLanguage"`
		VAPIDSubject        *string `json:"vapidSubject"`
		WeChatMode          *string `json:"wechatMode"`
		WeChatGatewayURL    *string `json:"wechatGatewayUrl"`
		WeChatToken         *string `json:"wechatBridgeToken"`
	}
	if err := body(w, r, &input); err != nil {
		return err
	}
	cfg, err := s.loadPlatformRuntimeConfig(r.Context())
	if err != nil {
		return err
	}
	if input.RegistrationEnabled != nil {
		cfg.Registration = *input.RegistrationEnabled
	}
	if input.PublicURL != nil {
		cfg.PublicURL = strings.TrimSpace(*input.PublicURL)
		if err := validatePublicURL(cfg.PublicURL); err != nil {
			return err
		}
	}
	if input.SMTPHost != nil {
		cfg.SMTPHost = strings.TrimSpace(*input.SMTPHost)
	}
	if input.SMTPPort != nil {
		cfg.SMTPPort = strings.TrimSpace(*input.SMTPPort)
		if cfg.SMTPPort != "" {
			if n, err := strconv.Atoi(cfg.SMTPPort); err != nil || n < 1 || n > 65535 {
				return bad(400, "VALIDATION_ERROR", "SMTP 端口无效")
			}
		}
	}
	if input.SMTPUser != nil {
		cfg.SMTPUser = strings.TrimSpace(*input.SMTPUser)
	}
	if input.SMTPFrom != nil {
		cfg.SMTPFrom = strings.TrimSpace(*input.SMTPFrom)
		if cfg.SMTPFrom != "" {
			if _, err := mail.ParseAddress(cfg.SMTPFrom); err != nil {
				return bad(400, "VALIDATION_ERROR", "SMTP 发件地址无效")
			}
		}
	}
	if input.SMTPTLSInsecure != nil {
		cfg.SMTPTLSInsecure = *input.SMTPTLSInsecure
	}
	if input.SMTPPassword != nil && strings.TrimSpace(*input.SMTPPassword) != "" {
		cfg.SMTPPassword, err = encryptAIKey("platform:smtp_password", strings.TrimSpace(*input.SMTPPassword))
		if err != nil {
			return err
		}
	}
	if input.STTProvider != nil {
		cfg.STTProvider = strings.TrimSpace(*input.STTProvider)
	}
	if input.STTBaseURL != nil {
		cfg.STTBaseURL = strings.TrimSpace(*input.STTBaseURL)
		if cfg.STTBaseURL != "" {
			if _, err := url.Parse(cfg.STTBaseURL); err != nil {
				return bad(400, "VALIDATION_ERROR", "STT 地址无效")
			}
		}
	}
	if input.STTAPIKey != nil && strings.TrimSpace(*input.STTAPIKey) != "" {
		cfg.STTAPIKey, err = encryptAIKey("platform:stt_api_key", strings.TrimSpace(*input.STTAPIKey))
		if err != nil {
			return err
		}
	}
	if input.STTModel != nil {
		cfg.STTModel = strings.TrimSpace(*input.STTModel)
	}
	if input.STTLanguage != nil {
		cfg.STTLanguage = strings.TrimSpace(*input.STTLanguage)
	}
	if input.VAPIDSubject != nil {
		cfg.VAPIDSubject = strings.TrimSpace(*input.VAPIDSubject)
		if cfg.VAPIDSubject != "" && !validVAPIDSubject(cfg.VAPIDSubject) {
			return bad(400, "VALIDATION_ERROR", "VAPID 联系地址必须是真实邮箱或 HTTPS 页面")
		}
	}
	if input.WeChatMode != nil {
		cfg.WeChatMode = strings.ToLower(strings.TrimSpace(*input.WeChatMode))
		if cfg.WeChatMode != "" && cfg.WeChatMode != "real" && cfg.WeChatMode != "unconfigured" {
			return bad(400, "VALIDATION_ERROR", "微信模式只能是 real 或 unconfigured")
		}
	}
	if input.WeChatGatewayURL != nil {
		cfg.WeChatGatewayURL = strings.TrimSpace(*input.WeChatGatewayURL)
		if err := validateWechatGatewayURL(cfg.WeChatGatewayURL); err != nil {
			return err
		}
	}
	if input.WeChatToken != nil && strings.TrimSpace(*input.WeChatToken) != "" {
		token := strings.TrimSpace(*input.WeChatToken)
		if len(token) < 32 {
			return bad(400, "VALIDATION_ERROR", "微信网关 token 至少 32 字符")
		}
		cfg.WeChatToken, err = encryptAIKey("platform:wechat_bridge_token", token)
		if err != nil {
			return err
		}
	}
	_, err = s.DB.ExecContext(r.Context(), `UPDATE platform_settings SET registration_enabled=?,public_url=?,smtp_host=?,smtp_port=?,smtp_user=?,smtp_password_ciphertext=?,smtp_from=?,smtp_tls_insecure=?,stt_provider=?,stt_base_url=?,stt_api_key_ciphertext=?,stt_model=?,stt_language=?,vapid_subject=?,wechat_mode=?,wechat_gateway_url=?,wechat_gateway_bridge_token_ciphertext=?,updated_by=UNHEX(?),updated_at=UTC_TIMESTAMP(3) WHERE id=1`,
		cfg.Registration, nullString(cfg.PublicURL), nullString(cfg.SMTPHost), nullString(cfg.SMTPPort), nullString(cfg.SMTPUser), cfg.SMTPPassword, nullString(cfg.SMTPFrom), cfg.SMTPTLSInsecure, nullString(cfg.STTProvider), nullString(cfg.STTBaseURL), cfg.STTAPIKey, nullString(cfg.STTModel), nullString(cfg.STTLanguage), nullString(cfg.VAPIDSubject), nullString(cfg.WeChatMode), nullString(cfg.WeChatGatewayURL), cfg.WeChatToken, u.ID)
	if err != nil {
		return err
	}
	if input.PublicURL != nil && cfg.PublicURL == "" {
		_ = os.Unsetenv("PUBLIC_URL")
	}
	if input.SMTPHost != nil && cfg.SMTPHost == "" {
		_ = os.Unsetenv("SMTP_HOST")
	}
	if input.SMTPPort != nil && cfg.SMTPPort == "" {
		_ = os.Unsetenv("SMTP_PORT")
	}
	if input.SMTPUser != nil && cfg.SMTPUser == "" {
		_ = os.Unsetenv("SMTP_USER")
	}
	if input.SMTPFrom != nil && cfg.SMTPFrom == "" {
		_ = os.Unsetenv("SMTP_FROM")
	}
	if input.STTBaseURL != nil && cfg.STTBaseURL == "" {
		_ = os.Unsetenv("STT_BASE_URL")
	}
	if input.STTProvider != nil && cfg.STTProvider == "" {
		_ = os.Unsetenv("STT_PROVIDER")
	}
	if input.VAPIDSubject != nil && cfg.VAPIDSubject == "" {
		_ = os.Unsetenv("VAPID_SUBJECT")
	}
	if input.WeChatGatewayURL != nil && cfg.WeChatGatewayURL == "" {
		_ = os.Unsetenv("WECHAT_GATEWAY_URL")
	}
	if err = s.applyPlatformRuntimeConfig(r.Context()); err != nil {
		return err
	}
	write(w, 200, platformSettingsView(cfg))
	return nil
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
