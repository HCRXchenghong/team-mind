package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// trafilaturaExtract shells out to the optional Python extractor. It is opt-in
// via TRAFILATURA_PYTHON so a Go-only deployment still works with the native
// fallback. The helper never receives cookies or credentials.
func trafilaturaExtract(ctx context.Context, pageURL, html string) (string, string, bool) {
	python := strings.TrimSpace(os.Getenv("TRAFILATURA_PYTHON"))
	if python == "" {
		return "", "", false
	}
	script := strings.TrimSpace(os.Getenv("TRAFILATURA_SCRIPT"))
	if script == "" {
		script = "scripts/web_extract.py"
	}
	if _, err := os.Stat(script); err != nil {
		return "", "", false
	}
	payload, _ := json.Marshal(map[string]string{"url": pageURL, "html": html})
	runCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, python, script)
	cmd.Stdin = bytes.NewReader(payload)
	if pyPath := strings.TrimSpace(os.Getenv("TRAFILATURA_PYTHONPATH")); pyPath != "" {
		cmd.Env = append(os.Environ(), "PYTHONPATH="+pyPath)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", false
	}
	var result struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if json.Unmarshal(stdout.Bytes(), &result) != nil || strings.TrimSpace(result.Text) == "" {
		return "", "", false
	}
	return truncateRunes(result.Title, 200), result.Text, true
}

// firecrawlScrape uses Firecrawl when an API key is configured. Native fetch
// remains the fallback so a missing/expired key cannot break web_fetch.
func firecrawlScrape(ctx context.Context, pageURL string) (string, string, error) {
	key := strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY"))
	if key == "" {
		return "", "", errors.New("firecrawl_not_configured")
	}
	base := strings.TrimSuffix(strings.TrimSpace(os.Getenv("FIRECRAWL_BASE_URL")), "/")
	if base == "" {
		base = "https://api.firecrawl.dev"
	}
	body, _ := json.Marshal(map[string]any{
		"url": pageURL, "formats": []string{"markdown"}, "onlyMainContent": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/scrape", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return "", "", errors.New("firecrawl_status")
	}
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Markdown string `json:"markdown"`
			Metadata struct {
				Title string `json:"title"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return "", "", err
	}
	if !result.Success || strings.TrimSpace(result.Data.Markdown) == "" {
		return "", "", errors.New("firecrawl_empty")
	}
	return truncateRunes(result.Data.Metadata.Title, 200), result.Data.Markdown, nil
}

func webExtractScriptPath() string {
	if path := strings.TrimSpace(os.Getenv("TRAFILATURA_SCRIPT")); path != "" {
		return path
	}
	path, _ := filepath.Abs("scripts/web_extract.py")
	return path
}
