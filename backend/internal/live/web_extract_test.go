package live

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFirecrawlScrapeParsesMarkdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/scrape" || r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"markdown": "# 标题\n\n正文 1024",
				"metadata": map[string]string{"title": "Firecrawl 标题"},
			},
		})
	}))
	defer server.Close()
	t.Setenv("FIRECRAWL_API_KEY", "test-key")
	t.Setenv("FIRECRAWL_BASE_URL", server.URL)
	title, text, err := firecrawlScrape(context.Background(), "https://example.com/a")
	if err != nil || title != "Firecrawl 标题" || !strings.Contains(text, "正文 1024") {
		t.Fatalf("firecrawl extraction failed: title=%q text=%q err=%v", title, text, err)
	}
}

func TestTrafilaturaExtractLive(t *testing.T) {
	if os.Getenv("TRAFILATURA_PYTHON") == "" {
		t.Skip("TRAFILATURA_PYTHON not configured")
	}
	output, err := runWebFetch(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("web fetch failed: %v", err)
	}
	if output["extractor"] != "trafilatura" || (strings.TrimSpace(output["text"].(string)) == "" && output["title"] == "") {
		t.Fatalf("trafilatura not used: %#v", output)
	}
}
