package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Reranker is deliberately separate from Embedder: retrieval may use one
// provider while relevance scoring uses another, and either can be disabled
// without changing the repository contract.
type Reranker interface {
	Rerank(ctx context.Context, query string, hits []QueryHit, topK int) ([]QueryHit, error)
	Model() string
}

type CohereReranker struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewCohereReranker(baseURL, apiKey, model string, client *http.Client) *CohereReranker {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if strings.TrimSpace(model) == "" {
		model = "rerank-v3.5"
	}
	return &CohereReranker{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		client:  client,
	}
}

func (r *CohereReranker) Model() string {
	if r == nil {
		return ""
	}
	return r.model
}

type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

func (r *CohereReranker) Rerank(ctx context.Context, query string, hits []QueryHit, topK int) ([]QueryHit, error) {
	if r == nil || r.baseURL == "" {
		return nil, fmt.Errorf("reranker is not configured")
	}
	query = strings.TrimSpace(query)
	if query == "" || len(hits) == 0 {
		return hits, nil
	}
	if topK <= 0 || topK > len(hits) {
		topK = len(hits)
	}
	documents := make([]string, len(hits))
	for i, hit := range hits {
		documents[i] = hit.Title + "\n" + hit.Heading + "\n" + hit.Content
	}
	body, err := json.Marshal(rerankRequest{Model: r.model, Query: query, Documents: documents, TopN: topK})
	if err != nil {
		return nil, fmt.Errorf("encode rerank request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.apiKey)
		req.Header.Set("X-API-Key", r.apiKey)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("reranker returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}
	out := make([]QueryHit, 0, minInt(topK, len(decoded.Results)))
	seen := make(map[int]struct{}, len(decoded.Results))
	for _, item := range decoded.Results {
		if item.Index < 0 || item.Index >= len(hits) {
			continue
		}
		if _, ok := seen[item.Index]; ok {
			continue
		}
		seen[item.Index] = struct{}{}
		hit := hits[item.Index]
		hit.Score = item.RelevanceScore
		out = append(out, hit)
		if len(out) == topK {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("reranker returned no valid results")
	}
	return out, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
