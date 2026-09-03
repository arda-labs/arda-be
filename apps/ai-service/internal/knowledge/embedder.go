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

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if e.baseURL == "" {
		return nil, fmt.Errorf("embedder base URL is not configured")
	}

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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(body))
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
