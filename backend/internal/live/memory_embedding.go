package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

func memoryEmbeddingConfig() (baseURL, model string, ok bool) {
	if strings.EqualFold(os.Getenv("MEMORY_EMBEDDING_DISABLED"), "true") {
		return "", "", false
	}
	baseURL = strings.TrimSuffix(os.Getenv("OLLAMA_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return "", "", false
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) && !strings.EqualFold(os.Getenv("MEMORY_EMBEDDING_ALLOW_REMOTE"), "true") {
		return "", "", false
	}
	model = strings.TrimSpace(os.Getenv("MEMORY_EMBEDDING_MODEL"))
	if model == "" {
		model = "nomic-embed-text"
	}
	return baseURL, model, true
}

func embedMemoryText(ctx context.Context, text string) ([]float32, error) {
	baseURL, model, ok := memoryEmbeddingConfig()
	if !ok {
		return nil, errors.New("memory_embedding_disabled")
	}
	payload, err := json.Marshal(map[string]any{"model": model, "input": truncateRunes(text, 4000)})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/embed", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 512))
		return nil, errors.New("memory_embedding_failed")
	}
	var output struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&output); err != nil {
		return nil, err
	}
	if len(output.Embeddings) != 1 || len(output.Embeddings[0]) == 0 {
		return nil, errors.New("memory_embedding_empty")
	}
	return output.Embeddings[0], nil
}

func decodeEmbedding(raw []byte) []float32 {
	if len(raw) == 0 {
		return nil
	}
	var vector []float32
	if json.Unmarshal(raw, &vector) != nil {
		return nil
	}
	return vector
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func rankMemoryCandidatesSemantic(items []memoryCandidate, queryEmbedding []float32, limit int) []memoryCandidate {
	if limit <= 0 || len(items) <= limit || len(queryEmbedding) == 0 {
		return items
	}
	type scored struct {
		item  memoryCandidate
		score float64
		index int
	}
	ranked := make([]scored, 0, len(items))
	withVector := 0
	for index, item := range items {
		score := 0.0
		if len(item.Embedding) == len(queryEmbedding) {
			score = cosineSimilarity(item.Embedding, queryEmbedding)
			withVector++
		}
		ranked = append(ranked, scored{item: item, score: score, index: index})
	}
	if withVector < 2 {
		return items
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if ranked[i].item.Importance != ranked[j].item.Importance {
			return ranked[i].item.Importance > ranked[j].item.Importance
		}
		return ranked[i].index < ranked[j].index
	})
	out := make([]memoryCandidate, 0, limit)
	for _, entry := range ranked {
		if len(out) >= limit {
			break
		}
		out = append(out, entry.item)
	}
	return out
}

// RunMemoryEmbeddingWorker backfills embeddings in the background. Chat never
// blocks on embedding every memory; it only ranks whatever is already indexed.
func (s *Server) RunMemoryEmbeddingWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	lastLog := time.Time{}
	for {
		if err := s.embedPendingMemories(ctx); err != nil && time.Since(lastLog) > 30*time.Second {
			log.Printf("memory embedding worker: %v", err)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) embedPendingMemories(ctx context.Context) error {
	type pending struct {
		table, id, content string
	}
	items := make([]pending, 0, 10)
	rows, err := s.DB.QueryContext(ctx, `SELECT 'user',HEX(id),content FROM user_memories
		WHERE deleted_at IS NULL AND confirmed_at IS NOT NULL AND sensitivity='normal' AND embedding IS NULL
		ORDER BY updated_at LIMIT 5`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item pending
		if err = rows.Scan(&item.table, &item.id, &item.content); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT 'team',HEX(id),content FROM team_memories
		WHERE deleted_at IS NULL AND confirmed_at IS NOT NULL AND sensitivity='normal' AND embedding IS NULL
		ORDER BY updated_at LIMIT 5`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item pending
		if err = rows.Scan(&item.table, &item.id, &item.content); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, item := range items {
		vector, embedErr := embedMemoryText(ctx, item.content)
		if embedErr != nil {
			return embedErr
		}
		encoded, _ := json.Marshal(vector)
		table := "user_memories"
		if item.table == "team" {
			table = "team_memories"
		}
		if _, err = s.DB.ExecContext(ctx, `UPDATE `+table+` SET embedding=? WHERE id=UNHEX(?)`, encoded, item.id); err != nil {
			return err
		}
	}
	return nil
}
