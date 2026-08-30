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

// Embedder turns document or query text into vectors. Model() names the
// embedding space so stored chunk vectors are only compared against queries
// from the same model (see ai_knowledge_chunks.embedding_model).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dimensions() int
}

const defaultEmbeddingTimeout = 30 * time.Second

// WorkersAIEmbedder calls a Cloudflare Workers AI embedding model
// (e.g. @cf/qwen/qwen3-embedding-0.6b) via the REST API.
type WorkersAIEmbedder struct {
	accountID string
	model     string
	apiToken  string
	client    *http.Client
}

func NewWorkersAIEmbedder(accountID, model, apiToken string, client *http.Client) *WorkersAIEmbedder {
	if client == nil {
		client = &http.Client{Timeout: defaultEmbeddingTimeout}
	}
	return &WorkersAIEmbedder{accountID: accountID, model: model, apiToken: apiToken, client: client}
}

func (e *WorkersAIEmbedder) Model() string { return e.model }

// Dimensions is declared by the chosen model family; qwen3-embedding and
// bge-m3 both emit 1024-dim dense vectors on Workers AI.
func (e *WorkersAIEmbedder) Dimensions() int { return 1024 }

func (e *WorkersAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil || e.accountID == "" || e.model == "" {
		return nil, fmt.Errorf("workers ai embedder is not configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]any{"text": texts})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s", e.accountID, e.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiToken)
	}

	response, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Result struct {
			Data [][]float32 `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if !parsed.Success {
		message := "unknown workers ai error"
		if len(parsed.Errors) > 0 {
			message = parsed.Errors[0].Message
		}
		return nil, fmt.Errorf("embedding rejected: %s", message)
	}
	if len(parsed.Result.Data) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: sent %d, got %d", len(texts), len(parsed.Result.Data))
	}
	return parsed.Result.Data, nil
}

// OpenAIEmbedder targets any OpenAI-compatible /embeddings endpoint, which is
// what self-hosted vLLM/Ollama expose — the self-host switch is this struct.
type OpenAIEmbedder struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func NewOpenAIEmbedder(baseURL, model, apiKey string, client *http.Client) *OpenAIEmbedder {
	if client == nil {
		client = &http.Client{Timeout: defaultEmbeddingTimeout}
	}
	return &OpenAIEmbedder{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), model: model, apiKey: apiKey, client: client}
}

func (e *OpenAIEmbedder) Model() string { return e.model }

func (e *OpenAIEmbedder) Dimensions() int { return 1024 }

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil || e.baseURL == "" || e.model == "" {
		return nil, fmt.Errorf("openai embedder is not configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	response, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<20))
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: sent %d, got %d", len(texts), len(parsed.Data))
	}
	vectors := make([][]float32, len(texts))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(vectors) {
			return nil, fmt.Errorf("embedding index out of range: %d", item.Index)
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}
