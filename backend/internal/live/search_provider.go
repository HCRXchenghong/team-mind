package live

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Search is a replaceable provider boundary. The native scraper is kept as a
// fallback, but production deployments should point SEARXNG_URL at a
// self-hosted SearXNG instance and let it aggregate engines, apply rate
// limits and return stable JSON. This mirrors how retrieval layers in
// commercial assistants isolate "search" from "fetch" and "rank".
type searchProviderPreference string

const (
	searchAuto     searchProviderPreference = "auto"
	searchSearXNG  searchProviderPreference = "searxng"
	searchNative   searchProviderPreference = "native"
	searxngTimeout                          = 15 * time.Second
)

func configuredSearchProvider() searchProviderPreference {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SEARCH_PROVIDER"))) {
	case "searxng":
		return searchSearXNG
	case "native":
		return searchNative
	default:
		return searchAuto
	}
}

func searxngBaseURL() (string, bool) {
	raw := strings.TrimSpace(os.Getenv("SEARXNG_URL"))
	if raw == "" {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	return strings.TrimSuffix(parsed.String(), "/"), true
}

type searxngPayload struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func searxngSearch(ctx context.Context, baseURL, query string, limit int) ([]webSearchResult, error) {
	endpoint := baseURL + "/search?" + url.Values{
		"q":          {query},
		"format":     {"json"},
		"language":   {"zh-CN"},
		"safesearch": {"1"},
		"pageno":     {"1"},
	}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "TeamMind/1.0 (+self-hosted retrieval)")
	// The endpoint is operator-configured and often loopback, so it must not
	// use the public-web SSRF guard reserved for model-supplied URLs.
	client := &http.Client{Timeout: searxngTimeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng returned %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var payload searxngPayload
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	results := make([]webSearchResult, 0, limit)
	seen := map[string]struct{}{}
	for _, item := range payload.Results {
		link := strings.TrimSpace(item.URL)
		title := cleanWebText(item.Title)
		if link == "" || title == "" || !strings.HasPrefix(link, "http") {
			continue
		}
		if isSearchEngineResultURL(link) {
			continue
		}
		if _, exists := seen[link]; exists {
			continue
		}
		seen[link] = struct{}{}
		results = append(results, webSearchResult{
			Title:   title,
			URL:     link,
			Snippet: truncateRunes(cleanWebText(item.Content), 320),
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// runSearchProvider tries the configured provider before the native scraper.
// auto = SearXNG when configured, otherwise native; searxng = fail closed
// instead of silently degrading; native = skip SearXNG.
func runSearchProvider(ctx context.Context, query string, limit int) ([]webSearchResult, string, bool, error) {
	preference := configuredSearchProvider()
	if preference == searchNative {
		return nil, "", false, nil
	}
	baseURL, configured := searxngBaseURL()
	if !configured {
		if preference == searchSearXNG {
			return nil, "", true, bad(http.StatusServiceUnavailable, "SEARCH_NOT_CONFIGURED", "SEARCH_PROVIDER=searxng 但未配置 SEARXNG_URL")
		}
		return nil, "", false, nil
	}
	results, err := searxngSearch(ctx, baseURL, query, limit)
	if err == nil && len(results) > 0 {
		log.Printf("web_search provider=searxng results=%d", len(results))
		return results, "searxng", true, nil
	}
	if preference == searchSearXNG {
		if err == nil {
			err = fmt.Errorf("searxng returned no usable results")
		}
		return nil, "", true, bad(http.StatusBadGateway, "SEARCH_FAILED", "SearXNG 检索失败："+truncateRunes(err.Error(), 160))
	}
	return nil, "", false, nil
}
