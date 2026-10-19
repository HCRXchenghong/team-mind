package live

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeWechatBaseURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"", "https://ilinkai.weixin.qq.com", true},
		{"https://ilinkai.weixin.qq.com", "https://ilinkai.weixin.qq.com", true},
		{"https://hk.weixin.qq.com/", "https://hk.weixin.qq.com", true},
		{"http://ilinkai.weixin.qq.com", "", false},
		{"https://weixin.qq.com.evil.example", "", false},
		{"https://user@ilinkai.weixin.qq.com", "", false},
		{"https://ilinkai.weixin.qq.com:8443", "", false},
		{"https://ilinkai.weixin.qq.com/path", "", false},
	}
	for _, test := range tests {
		got, err := normalizeWechatBaseURL(test.input)
		if (err == nil) != test.ok || got != test.want {
			t.Errorf("normalizeWechatBaseURL(%q) = %q, %v; want %q, ok=%t", test.input, got, err, test.want, test.ok)
		}
	}
}

func TestDecodeWechatCredentialsSupportsEncryptedLegacyAndPluginFormats(t *testing.T) {
	legacy, err := decodeWechatCredentials("legacy-token", "https://ilinkai.weixin.qq.com")
	if err != nil || legacy.Token != "legacy-token" || legacy.BaseURL != "https://ilinkai.weixin.qq.com" {
		t.Fatalf("legacy credentials did not load: %+v err=%v", legacy, err)
	}
	plugin, err := decodeWechatCredentials(`{"token":"plugin-token","baseUrl":"https://hk.weixin.qq.com"}`, "https://ilinkai.weixin.qq.com")
	if err != nil || plugin.Token != "plugin-token" || plugin.BaseURL != "https://hk.weixin.qq.com" {
		t.Fatalf("plugin credentials did not load: %+v err=%v", plugin, err)
	}
	if _, err = decodeWechatCredentials(`{"token":"plugin-token","baseUrl":"https://evil.example"}`, "https://ilinkai.weixin.qq.com"); err == nil {
		t.Fatal("credential parser accepted a non-Tencent endpoint")
	}
}

func TestOpenClawWechatGatewayUsesAuthenticatedNarrowBridge(t *testing.T) {
	const bridgeToken = "unit-test-bridge-token-that-is-long-enough"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/teammind-wechat-bridge/v1/channel/updates" {
			t.Errorf("unexpected bridge route %q", r.URL.Path)
		}
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+bridgeToken {
			t.Errorf("bridge request was not authenticated POST")
		}
		var input map[string]string
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode bridge request: %v", err)
		}
		if input["token"] != "channel-secret" || input["cursor"] != "cursor-before" || input["accountId"] != strings.Repeat("a", 32) {
			t.Errorf("unexpected bridge request fields")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0,"errcode":0,"msgs":[{"message_id":"18446744073709551615","from_user_id":"sender","message_type":1,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"hello"}}]}],"get_updates_buf":"cursor-after"}`))
	}))
	defer server.Close()
	gateway := &openClawWechatGateway{
		baseURL: strings.TrimSuffix(server.URL, "/") + "/teammind-wechat-bridge/v1",
		token:   bridgeToken,
		client:  server.Client(),
	}
	updates, err := gateway.getUpdates(context.Background(), strings.Repeat("a", 32), "https://ilinkai.weixin.qq.com", "channel-secret", "cursor-before")
	if err != nil {
		t.Fatal(err)
	}
	if updates.Ret != 0 || updates.Buf != "cursor-after" || len(updates.Messages) != 1 {
		t.Fatalf("unexpected plugin update response: %+v", updates)
	}
	if updates.Messages[0].MessageID.String() != "18446744073709551615" || inboundText(updates.Messages[0]) != "hello" {
		t.Fatalf("message identity or text lost in bridge: %+v", updates.Messages[0])
	}
	if err = gateway.post(context.Background(), "admin/config", map[string]string{}, &map[string]any{}); err == nil {
		t.Fatal("gateway allowed an operation outside the explicit bridge allowlist")
	}
}

func TestWechatGatewayConfigurationRequiresNumericLoopback(t *testing.T) {
	t.Setenv("WECHAT_GATEWAY_BRIDGE_TOKEN", strings.Repeat("x", 32))
	for _, raw := range []string{
		"http://example.com:18793/teammind-wechat-bridge/v1",
		"http://localhost:18793/teammind-wechat-bridge/v1",
		"http://127.0.0.1:18793/other",
	} {
		t.Setenv("WECHAT_GATEWAY_URL", raw)
		if _, err := newOpenClawWechatGateway(); err == nil {
			t.Errorf("accepted unsafe or unsupported gateway URL %q", raw)
		}
	}
	t.Setenv("WECHAT_GATEWAY_URL", "http://127.0.0.1:18793/teammind-wechat-bridge/v1")
	if _, err := newOpenClawWechatGateway(); err != nil {
		t.Fatalf("rejected valid loopback gateway: %v", err)
	}
}
