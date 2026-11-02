package live

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearxngSearchParsesFiltersAndDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("format") != "json" || r.URL.Query().Get("q") != "团队协作" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
			{"title": "甲", "url": "https://a.example/1", "content": "第一条"},
			{"title": "搜索页", "url": "https://ai.so.com/search?q=x", "content": "应被过滤"},
			{"title": "百科", "url": "https://baike.baidu.com/item/x", "content": "百科条目"},
			{"title": "甲重复", "url": "https://a.example/1", "content": "重复"},
		}})
	}))
	defer server.Close()
	results, err := searxngSearch(context.Background(), server.URL, "团队协作", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(results), results)
	}
	if results[0].URL != "https://a.example/1" || results[1].URL != "https://baike.baidu.com/item/x" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearchProviderPreferenceAndConfig(t *testing.T) {
	t.Setenv("SEARCH_PROVIDER", "searxng")
	if got := configuredSearchProvider(); got != searchSearXNG {
		t.Fatalf("preference = %q", got)
	}
	t.Setenv("SEARCH_PROVIDER", "bogus")
	if got := configuredSearchProvider(); got != searchAuto {
		t.Fatalf("invalid preference = %q", got)
	}
	t.Setenv("SEARXNG_URL", "file:///etc/passwd")
	if _, ok := searxngBaseURL(); ok {
		t.Fatal("non-http SEARXNG_URL accepted")
	}
	t.Setenv("SEARXNG_URL", "http://user:pass@127.0.0.1:8080")
	if _, ok := searxngBaseURL(); ok {
		t.Fatal("userinfo SEARXNG_URL accepted")
	}
	t.Setenv("SEARXNG_URL", "http://127.0.0.1:8080/")
	if got, ok := searxngBaseURL(); !ok || got != "http://127.0.0.1:8080" {
		t.Fatalf("base URL = %q, %v", got, ok)
	}
}

func TestRunSearchProviderFailsClosedWhenSearxngRequired(t *testing.T) {
	t.Setenv("SEARCH_PROVIDER", "searxng")
	t.Setenv("SEARXNG_URL", "")
	_, _, attempted, err := runSearchProvider(context.Background(), "x", 5)
	if !attempted || err == nil {
		t.Fatalf("attempted=%v err=%v, want fail-closed", attempted, err)
	}
	if !strings.Contains(err.Error(), "SEARCH_NOT_CONFIGURED") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunSearchProviderUsesSearxngWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
			{"title": "来源", "url": "https://source.example/a", "content": "摘要"},
		}})
	}))
	defer server.Close()
	t.Setenv("SEARCH_PROVIDER", "auto")
	t.Setenv("SEARXNG_URL", server.URL)
	results, source, attempted, err := runSearchProvider(context.Background(), "查询", 5)
	if err != nil || !attempted || source != "searxng" || len(results) != 1 {
		t.Fatalf("results=%v source=%q attempted=%v err=%v", results, source, attempted, err)
	}
}

func TestSearchEngineResultURLAllowsBaiduBaike(t *testing.T) {
	if isSearchEngineResultURL("https://baike.baidu.com/item/OpenAI") {
		t.Fatal("baike.baidu.com should be a valid source")
	}
	if !isSearchEngineResultURL("https://baidu.com/link?url=x") {
		t.Fatal("baidu redirector should be blocked")
	}
}
