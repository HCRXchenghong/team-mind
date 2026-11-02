package live

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Web tools are explicit, read-only capabilities: they never touch team data
// and they are offered only when the caller authorized web access for this
// agent run. Every network destination is resolved and checked against private
// address ranges before a connection is opened, so a prompt-injected URL
// cannot reach the loopback API, MySQL, Docker, or the host network.
const (
	webSearchDefaultResults = 5
	webSearchMaxResults     = 8
	webFetchByteLimit       = 480 * 1024
	webFetchCharLimit       = 16000
	webToolTimeout          = 20 * time.Second
	webSearchTimeout        = 15 * time.Second
)

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type webSearchArgs struct {
	Query      string `json:"query"`
	MaxResults int    `json:"maxResults,omitempty"`
}

type webFetchArgs struct {
	URL string `json:"url"`
}

var (
	bingTitleLink  = regexp.MustCompile(`(?is)<h2[^>]*>\s*<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	bingSnippet    = regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	soTitleLink    = regexp.MustCompile(`(?is)<h3 class="res-title[^"]*"[^>]*>\s*<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	soRealURL      = regexp.MustCompile(`(?is)data-mdurl="([^"]+)"`)
	soSnippet      = regexp.MustCompile(`(?is)<p class="res-desc"[^>]*>(.*?)</p>`)
	htmlNoise      = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<noscript[^>]*>.*?</noscript>|<template[^>]*>.*?</template>|<svg[^>]*>.*?</svg>`)
	htmlComments   = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlLineBreaks = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/tr|/h[1-6]|/section|/article|/table)[^>]*>`)
	htmlTags       = regexp.MustCompile(`(?s)<[^>]+>`)
	inlineSpaces   = regexp.MustCompile(`[ \t\f\v\x{00a0}]+`)
	manyNewlines   = regexp.MustCompile(`\n{3,}`)
)

func agentWebToolsEnabled() bool { return true }

func runWebSearch(ctx context.Context, rawQuery string, maxResults int) (map[string]any, error) {
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		return nil, bad(http.StatusBadRequest, "WEB_SEARCH_INVALID", "搜索关键词不能为空")
	}
	if len([]rune(query)) > 400 {
		return nil, bad(http.StatusBadRequest, "WEB_SEARCH_INVALID", "搜索关键词过长")
	}
	if maxResults <= 0 {
		maxResults = webSearchDefaultResults
	}
	// Models tend to pass the schema minimum. Three results is the smallest
	// useful evidence set; the caller can still request up to eight.
	if maxResults < 3 {
		maxResults = 3
	}
	if maxResults > webSearchMaxResults {
		maxResults = webSearchMaxResults
	}
	ctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()
	if providerResults, providerSource, attempted, providerErr := runSearchProvider(ctx, query, maxResults); attempted {
		if providerErr != nil {
			return nil, providerErr
		}
		if len(providerResults) > 0 {
			return map[string]any{
				"query":   query,
				"count":   len(providerResults),
				"results": providerResults,
				"source":  providerSource,
			}, nil
		}
	}
	results, source := make([]webSearchResult, 0, maxResults), "so"
	primary, primaryErr := soWebSearch(ctx, query, maxResults)
	results = append(results, primary...)
	if len(results) < 3 {
		fallback, fallbackErr := bingWebSearch(ctx, query, maxResults-len(results))
		if fallbackErr == nil {
			results = mergeWebResults(results, fallback, maxResults)
			if len(primary) > 0 {
				source = "so+bing"
			} else {
				source = "bing"
			}
		}
		if len(results) == 0 {
			if primaryErr != nil && fallbackErr != nil {
				return nil, primaryErr
			}
			return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", "搜索服务没有返回结果，请稍后重试")
		}
	}
	return map[string]any{
		"query":   query,
		"count":   len(results),
		"results": results,
		"source":  source,
	}, nil
}

// soWebSearch uses 360 Search, which keeps a usable Chinese index and exposes
// the real destination in data-mdurl. Bing is kept as a fallback because
// engines rate-limit or degrade results independently.
func soWebSearch(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	endpoint := "https://www.so.com/s?" + url.Values{"q": {query}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", "搜索请求构造失败")
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.6")
	response, err := webHTTPClient().Do(request)
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", "无法连接搜索服务，请检查本机网络")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", fmt.Sprintf("搜索服务返回 %d", response.StatusCode))
	}
	document, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", "读取搜索结果失败")
	}
	results := make([]webSearchResult, 0, maxResults)
	seen := map[string]struct{}{}
	for _, block := range strings.Split(string(document), `<li class="res-list"`)[1:] {
		if len(block) > 16000 {
			block = block[:16000]
		}
		titleMatch := soTitleLink.FindStringSubmatch(block)
		if len(titleMatch) < 3 {
			continue
		}
		link := ""
		if realMatch := soRealURL.FindStringSubmatch(block); len(realMatch) > 1 {
			link = html.UnescapeString(realMatch[1])
		} else if href := html.UnescapeString(titleMatch[1]); !strings.Contains(href, "so.com/link") {
			// Redirect-only links are useless as citations; skip them so the
			// model never quotes an opaque tracking URL.
			link = href
		}
		if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
			continue
		}
		if isSearchEngineResultURL(link) {
			// Citing 360/Bing search pages is not a real source and also leaks
			// an opaque tracking URL into the answer.
			continue
		}
		title := cleanWebText(titleMatch[2])
		if title == "" {
			continue
		}
		snippet := ""
		if match := soSnippet.FindStringSubmatch(block); len(match) > 1 {
			snippet = cleanWebText(match[1])
		}
		if _, exists := seen[link]; exists {
			continue
		}
		seen[link] = struct{}{}
		results = append(results, webSearchResult{Title: title, URL: link, Snippet: truncateRunes(snippet, 320)})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func isSearchEngineResultURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	path := strings.ToLower(parsed.Path)
	// Search result pages and redirectors, not every page hosted by a search
	// company: baike.baidu.com and similar encyclopedic hosts are valid sources.
	searchHosts := map[string]bool{
		"so.com": true, "www.so.com": true, "ai.so.com": true,
		"bing.com": true, "www.bing.com": true, "cn.bing.com": true,
		"duckduckgo.com": true, "html.duckduckgo.com": true, "lite.duckduckgo.com": true,
		"google.com": true, "www.google.com": true,
		"baidu.com": true, "www.baidu.com": true, "m.baidu.com": true,
	}
	if !searchHosts[host] {
		return false
	}
	for _, prefix := range []string{"/search", "/s", "/link", "/url", "/ck/a"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func mergeWebResults(primary, secondary []webSearchResult, limit int) []webSearchResult {
	merged := make([]webSearchResult, 0, limit)
	seen := map[string]struct{}{}
	for _, list := range [][]webSearchResult{primary, secondary} {
		for _, item := range list {
			if _, exists := seen[item.URL]; exists {
				continue
			}
			seen[item.URL] = struct{}{}
			merged = append(merged, item)
			if len(merged) >= limit {
				return merged
			}
		}
	}
	return merged
}

func bingWebSearch(ctx context.Context, query string, maxResults int) ([]webSearchResult, error) {
	endpoint := "https://www.bing.com/search?" + url.Values{
		"q":       {query},
		"setlang": {"zh-Hans"},
		"cc":      {"CN"},
	}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", "搜索请求构造失败")
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.6")
	response, err := webHTTPClient().Do(request)
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", "无法连接搜索服务，请检查本机网络")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", fmt.Sprintf("搜索服务返回 %d", response.StatusCode))
	}
	document, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_SEARCH_FAILED", "读取搜索结果失败")
	}
	results := make([]webSearchResult, 0, maxResults)
	seen := map[string]struct{}{}
	// Split on the result container instead of matching "<li>...</li>": Bing
	// nests lists inside some results, so a non-greedy element match would stop
	// at the first inner closing tag and silently drop most results.
	for _, block := range strings.Split(string(document), `<li class="b_algo"`)[1:] {
		if len(block) > 16000 {
			block = block[:16000]
		}
		titleMatch := bingTitleLink.FindStringSubmatch(block)
		if len(titleMatch) < 3 {
			continue
		}
		link := decodeBingRedirect(html.UnescapeString(titleMatch[1]))
		if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
			continue
		}
		if isSearchEngineResultURL(link) {
			// Citing 360/Bing search pages is not a real source and also leaks
			// an opaque tracking URL into the answer.
			continue
		}
		title := cleanWebText(titleMatch[2])
		if title == "" {
			continue
		}
		snippet := ""
		if match := bingSnippet.FindStringSubmatch(block); len(match) > 1 {
			snippet = cleanWebText(match[1])
		}
		if _, exists := seen[link]; exists {
			continue
		}
		seen[link] = struct{}{}
		results = append(results, webSearchResult{Title: title, URL: link, Snippet: truncateRunes(snippet, 320)})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

// Bing wraps organic result links in a redirector whose u parameter holds an
// a1-prefixed base64url target. Recover the real URL so the model can cite and
// fetch it directly.
func decodeBingRedirect(raw string) string {
	if !strings.Contains(raw, "bing.com/ck/a") {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	encoded := parsed.Query().Get("u")
	if !strings.HasPrefix(encoded, "a1") {
		return raw
	}
	encoded = encoded[2:]
	if padding := len(encoded) % 4; padding != 0 {
		encoded += strings.Repeat("=", 4-padding)
	}
	decoded, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return raw
	}
	target := strings.TrimSpace(string(decoded))
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}
	return raw
}

func runWebFetch(ctx context.Context, rawURL string) (map[string]any, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rawURL) > 2048 {
		return nil, bad(http.StatusBadRequest, "WEB_FETCH_INVALID", "URL 无效")
	}
	parsed, err := parsePublicWebURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, webToolTimeout)
	defer cancel()
	if os.Getenv("FIRECRAWL_API_KEY") != "" {
		if title, markdown, crawlErr := firecrawlScrape(ctx, parsed.String()); crawlErr == nil {
			truncated := utf8.RuneCountInString(markdown) > webFetchCharLimit
			if truncated {
				markdown = truncateRunes(markdown, webFetchCharLimit)
			}
			return map[string]any{
				"url": parsed.String(), "title": title, "text": strings.TrimSpace(markdown),
				"truncated": truncated, "chars": utf8.RuneCountInString(markdown), "extractor": "firecrawl",
			}, nil
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_FETCH_FAILED", "请求构造失败")
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	request.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.5")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.6")
	response, err := webHTTPClient().Do(request)
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_FETCH_FAILED", "页面无法访问，可能是网络受限或站点拒绝抓取")
	}
	defer response.Body.Close()
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, bad(http.StatusBadGateway, "WEB_FETCH_FAILED", fmt.Sprintf("页面返回 %d", response.StatusCode))
	}
	if contentType != "" && !strings.Contains(contentType, "text/") && !strings.Contains(contentType, "json") && !strings.Contains(contentType, "xml") {
		return nil, bad(http.StatusBadGateway, "WEB_FETCH_UNSUPPORTED", "只支持读取文本、HTML、JSON 或 XML 页面")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, webFetchByteLimit+1))
	if err != nil {
		return nil, bad(http.StatusBadGateway, "WEB_FETCH_FAILED", "读取页面失败")
	}
	truncated := len(data) > webFetchByteLimit
	if truncated {
		data = data[:webFetchByteLimit]
	}
	text := string(data)
	title := ""
	extractor := "native"
	if strings.Contains(contentType, "html") || looksLikeHTML(text) {
		title = extractHTMLTitle(text)
		if extractedTitle, extractedText, ok := trafilaturaExtract(ctx, parsed.String(), text); ok {
			if extractedTitle != "" {
				title = extractedTitle
			}
			text = extractedText
			extractor = "trafilatura"
		} else {
			text = htmlToText(text)
		}
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > webFetchCharLimit {
		text = truncateRunes(text, webFetchCharLimit)
		truncated = true
	}
	return map[string]any{
		"url":       parsed.String(),
		"title":     title,
		"text":      text,
		"truncated": truncated,
		"chars":     utf8.RuneCountInString(text),
		"extractor": extractor,
	}, nil
}

func looksLikeHTML(value string) bool {
	lower := strings.ToLower(value[:minInt(len(value), 2048)])
	return strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype html") || strings.Contains(lower, "<body")
}

func extractHTMLTitle(document string) string {
	match := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(document)
	if len(match) < 2 {
		return ""
	}
	return truncateRunes(cleanWebText(match[1]), 200)
}

func htmlToText(document string) string {
	text := htmlNoise.ReplaceAllString(document, " ")
	text = htmlComments.ReplaceAllString(text, " ")
	text = htmlLineBreaks.ReplaceAllString(text, "\n")
	text = htmlTags.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = inlineSpaces.ReplaceAllString(text, " ")
	lines := strings.Split(text, "\n")
	kept := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			kept = append(kept, line)
		}
	}
	text = strings.Join(kept, "\n")
	return manyNewlines.ReplaceAllString(text, "\n\n")
}

func cleanWebText(fragment string) string {
	return truncateRunes(htmlToText(fragment), 500)
}

func parsePublicWebURL(ctx context.Context, raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, bad(http.StatusBadRequest, "WEB_FETCH_INVALID", "URL 无效")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, bad(http.StatusBadRequest, "WEB_FETCH_INVALID", "只支持 http 或 https 地址")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".localhost") {
		return nil, bad(http.StatusBadRequest, "WEB_FETCH_BLOCKED", "该地址不允许访问")
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addresses) == 0 {
		return nil, bad(http.StatusBadGateway, "WEB_FETCH_FAILED", "无法解析该域名")
	}
	for _, address := range addresses {
		if !isPublicWebIP(address.IP) {
			return nil, bad(http.StatusBadRequest, "WEB_FETCH_BLOCKED", "该地址不允许访问")
		}
	}
	return parsed, nil
}

func isPublicWebIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 0 {
			return false
		}
		// Carrier-grade NAT and benchmark ranges are not reachable internet hosts.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
		if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
			return false
		}
	}
	return true
}

func webHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			dialer := &net.Dialer{Timeout: 8 * time.Second}
			var lastErr error
			for _, address := range addresses {
				if !isPublicWebIP(address.IP) {
					continue
				}
				connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			if lastErr == nil {
				lastErr = errors.New("目标地址不允许访问")
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout: 8 * time.Second,
		DisableKeepAlives:   true,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   webToolTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("重定向次数过多")
			}
			_, err := parsePublicWebURL(request.Context(), request.URL.String())
			return err
		},
	}
}
