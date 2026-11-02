package live

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeBingRedirectRecoversTarget(t *testing.T) {
	target := "https://example.com/docs?q=1"
	encoded := base64.URLEncoding.EncodeToString([]byte(target))
	encoded = strings.TrimRight(encoded, "=")
	wrapped := "https://www.bing.com/ck/a?!&&p=abc&u=a1" + encoded + "&ntb=1"
	if got := decodeBingRedirect(wrapped); got != target {
		t.Fatalf("decoded %q, want %q", got, target)
	}
	if got := decodeBingRedirect("https://example.com/plain"); got != "https://example.com/plain" {
		t.Fatalf("plain URL changed: %q", got)
	}
}

func TestHTMLToTextDropsScriptsAndMarkup(t *testing.T) {
	document := `<html><head><title>标题</title><style>body{color:red}</style></head>
<body><script>alert('x')</script><h1>你好</h1><p>第一段 &amp; 内容</p><div>第二段</div></body></html>`
	text := htmlToText(document)
	if strings.Contains(text, "alert") || strings.Contains(text, "color:red") || strings.Contains(text, "<") {
		t.Fatalf("markup leaked into text: %q", text)
	}
	for _, want := range []string{"你好", "第一段 & 内容", "第二段"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text %q lacks %q", text, want)
		}
	}
	if title := extractHTMLTitle(document); title != "标题" {
		t.Fatalf("title = %q", title)
	}
}

func TestParsePublicWebURLBlocksInternalTargets(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1:8188/readyz",
		"http://localhost/admin",
		"http://192.168.1.1/",
		"http://10.0.0.5/",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
		"http://[::1]/",
		"http://user:pass@example.com/",
	}
	for _, raw := range blocked {
		if _, err := parsePublicWebURL(context.Background(), raw); err == nil {
			t.Errorf("URL %q should be blocked", raw)
		}
	}
	if _, err := parsePublicWebURL(context.Background(), "https://example.com/"); err != nil {
		t.Errorf("public URL rejected: %v", err)
	}
}

func TestWebSourcesSuffixBuildsDeterministicCitations(t *testing.T) {
	search := map[string]any{"results": []webSearchResult{
		{Title: "甲", URL: "https://a.example/1"},
		{Title: "乙", URL: "https://b.example/2"},
	}}
	got := webSourcesSuffix("web_search", search)
	if !strings.Contains(got, "https://a.example/1") || !strings.Contains(got, "https://b.example/2") {
		t.Fatalf("search suffix lacks URLs: %q", got)
	}
	if fetch := webSourcesSuffix("web_fetch", map[string]any{"url": "https://c.example"}); !strings.Contains(fetch, "https://c.example") {
		t.Fatalf("fetch suffix lacks URL: %q", fetch)
	}
	if webSourcesSuffix("create_task", map[string]any{}) != "" {
		t.Fatal("non-web tool must not produce citations")
	}
}

func TestSearchEngineURLsAreNotCitations(t *testing.T) {
	for _, value := range []string{"https://ai.so.com/search?q=x", "https://www.bing.com/search?q=x", "https://baidu.com/link?url=x"} {
		if !isSearchEngineResultURL(value) {
			t.Errorf("search engine URL was accepted: %s", value)
		}
	}
	if isSearchEngineResultURL("https://www.psbc.com/cn/grfw/") {
		t.Error("real source was rejected")
	}
	results := map[string]any{"results": []webSearchResult{{Title: "邮储", URL: "https://www.psbc.com/a"}}}
	if replyCitesRealSource("来源：https://ai.so.com/search?q=x", "web_search", results) {
		t.Error("search page URL counted as a real citation")
	}
	if !replyCitesRealSource("来源：https://www.psbc.com/a", "web_search", results) {
		t.Error("real result URL was not detected")
	}
}
