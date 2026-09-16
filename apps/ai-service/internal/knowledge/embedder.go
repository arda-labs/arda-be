package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dimensions() int
}

type OpenAIEmbedder struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

func NewOpenAIEmbedder(baseURL, apiKey, model string, dimensions int, client *http.Client) *OpenAIEmbedder {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if dimensions <= 0 {
		dimensions = 1024
	}
	if model == "" {
		model = "@cf/qwen/qwen3-embedding-0.6b"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &OpenAIEmbedder{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		client:     client,
	}
}

func (e *OpenAIEmbedder) Model() string {
	return e.model
}

func (e *OpenAIEmbedder) Dimensions() int {
	return e.dimensions
}

type embeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type embeddingResponse struct {
	Data []embeddingData `json:"data"`
}

// EmbeddingStatusError is a non-2xx provider response. 4xx (other than 429) is
// a deterministic rejection and is not retried; 429/5xx are.
type EmbeddingStatusError struct {
	StatusCode int
	Body       string
}

func (e *EmbeddingStatusError) Error() string {
	return fmt.Sprintf("embedding API error (status %d): %s", e.StatusCode, e.Body)
}

const (
	// maxEmbedAttempts bounds retries for a single Embed call.
	maxEmbedAttempts = 2
	// maxAttemptBudget caps one attempt so a retry always has room; without a
	// cap a slow provider can consume the whole tool deadline on attempt one.
	maxAttemptBudget = 4 * time.Second
	// minAttemptBudget keeps the split from degenerating near the deadline.
	minAttemptBudget = 500 * time.Millisecond
	// embedRetryBackoff is the pause before a retry.
	embedRetryBackoff = 200 * time.Millisecond
)

// Embed returns one vector per input text. Provider spikes above 3 s were
// observed in production (2026-09-16), so a failed attempt is retried once
// within the same caller deadline, with the deadline split evenly between
// attempts (capped at maxAttemptBudget) instead of letting attempt one consume
// the whole budget.
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if e.baseURL == "" {
		return nil, fmt.Errorf("embedder base URL is not configured")
	}

	var lastErr error
	for attempt := 0; attempt < maxEmbedAttempts; attempt++ {
		attemptCtx, cancel := e.attemptContext(ctx, attempt)
		vectors, err := e.doEmbed(attemptCtx, texts)
		cancel()
		if err == nil {
			return vectors, nil
		}
		lastErr = err
		if ctx.Err() != nil || !retryableEmbedError(err) {
			break
		}
		select {
		case <-time.After(embedRetryBackoff):
		case <-ctx.Done():
			return nil, lastErr
		}
	}
	return nil, lastErr
}

// attemptContext gives each attempt an equal share of the caller deadline
// (bounded by maxAttemptBudget/minAttemptBudget). Without a caller deadline the
// parent context is used and the HTTP client timeout applies.
func (e *OpenAIEmbedder) attemptContext(ctx context.Context, attempt int) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.WithCancel(ctx)
	}
	share := remaining / time.Duration(maxEmbedAttempts-attempt)
	if share > maxAttemptBudget {
		share = maxAttemptBudget
	}
	if share < minAttemptBudget {
		share = minAttemptBudget
	}
	return context.WithTimeout(ctx, share)
}

func retryableEmbedError(err error) bool {
	var statusErr *EmbeddingStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusTooManyRequests || statusErr.StatusCode >= 500
	}
	return true
}

func (e *OpenAIEmbedder) doEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	url := e.baseURL
	if !strings.HasSuffix(url, "/embeddings") {
		url += "/embeddings"
	}

	reqBody, err := json.Marshal(embeddingRequest{
		Input: texts,
		Model: e.model,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute embedding request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, &EmbeddingStatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var res embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	if len(res.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d vectors, got %d", len(texts), len(res.Data))
	}

	vectors := make([][]float32, len(texts))
	for _, item := range res.Data {
		if item.Index >= 0 && item.Index < len(vectors) {
			vectors[item.Index] = item.Embedding
		}
	}
	return vectors, nil
}
