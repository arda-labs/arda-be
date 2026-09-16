package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func embeddingResponseBody(values ...float32) string {
	items := make([]map[string]any, len(values))
	for i, value := range values {
		items[i] = map[string]any{"embedding": []float32{value}, "index": i}
	}
	raw, _ := json.Marshal(map[string]any{"data": items})
	return string(raw)
}

// The provider occasionally spikes above the tool deadline; the first attempt
// must not consume the whole budget, or the retry has nothing left to run in.
func TestEmbedSplitsDeadlineAndRetries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			time.Sleep(800 * time.Millisecond) // cancelled by the attempt budget
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(embeddingResponseBody(0.1)))
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.URL, "k", "m", 1, server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	vectors, err := embedder.Embed(ctx, []string{"quy trình mở sổ tiết kiệm"})
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 1 {
		t.Fatalf("unexpected vectors: %v", vectors)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestEmbedRetriesServerErrors(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "upstream boom", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(embeddingResponseBody(0.2)))
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.URL, "k", "m", 1, server.Client())
	if _, err := embedder.Embed(context.Background(), []string{"q"}); err != nil {
		t.Fatalf("expected retry to recover, got %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestEmbedDoesNotRetryClientErrors(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "bad model", http.StatusBadRequest)
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.URL, "k", "m", 1, server.Client())
	if _, err := embedder.Embed(context.Background(), []string{"q"}); err == nil {
		t.Fatal("expected a 400 to fail")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("4xx must not be retried, got %d attempts", got)
	}
}

type countingEmbedder struct {
	calls int
	dims  int
}

func (c *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	c.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		vector := make([]float32, c.dims)
		vector[0] = float32(c.calls)
		out[i] = vector
	}
	return out, nil
}

func (c *countingEmbedder) Model() string   { return "counting" }
func (c *countingEmbedder) Dimensions() int { return c.dims }

func TestCachedEmbedderServesRepeatedQueries(t *testing.T) {
	inner := &countingEmbedder{dims: 2}
	embedder := NewCachedEmbedder(inner, NewMemoryEmbeddingCache(8), time.Minute)

	if _, err := embedder.Embed(context.Background(), []string{"nghỉ phép năm"}); err != nil {
		t.Fatalf("first embed: %v", err)
	}
	if _, err := embedder.Embed(context.Background(), []string{" nghỉ phép năm "}); err != nil {
		t.Fatalf("second embed: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected the cache to serve the second call, inner calls = %d", inner.calls)
	}
}

func TestCachedEmbedderBypassesBatches(t *testing.T) {
	inner := &countingEmbedder{dims: 2}
	embedder := NewCachedEmbedder(inner, NewMemoryEmbeddingCache(8), time.Minute)

	if _, err := embedder.Embed(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("batch embed: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("batch calls go straight through, inner calls = %d", inner.calls)
	}
}

func TestEmbeddingCacheKeyDependsOnModel(t *testing.T) {
	if EmbeddingCacheKey("model-a", "q") == EmbeddingCacheKey("model-b", "q") {
		t.Fatal("different models must not share cache entries")
	}
	if EmbeddingCacheKey("m", "q") == EmbeddingCacheKey("m", "other") {
		t.Fatal("different texts must not share cache entries")
	}
}

func TestMemoryEmbeddingCacheExpires(t *testing.T) {
	cache := NewMemoryEmbeddingCache(4)
	cache.Set(context.Background(), "k", []float32{1}, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, ok := cache.Get(context.Background(), "k"); ok {
		t.Fatal("expected the entry to expire")
	}
}
