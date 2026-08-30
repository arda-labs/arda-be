package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkersAIEmbedderMetadata(t *testing.T) {
	// The Workers AI endpoint is hardcoded to api.cloudflare.com, so this test
	// covers construction, metadata, and the empty-input no-op. The HTTP shape
	// is exercised by TestOpenAIEmbedder, which shares the same flow.
	embedder := NewWorkersAIEmbedder("acct", "@cf/qwen/qwen3-embedding-0.6b", "cf-token", nil)
	if embedder.Model() != "@cf/qwen/qwen3-embedding-0.6b" || embedder.Dimensions() != 1024 {
		t.Fatalf("embedder metadata wrong: %s %d", embedder.Model(), embedder.Dimensions())
	}
	if _, err := embedder.Embed(context.Background(), nil); err != nil {
		t.Fatalf("nil texts should be a no-op, got %v", err)
	}
	if _, err := NewWorkersAIEmbedder("", "m", "t", nil).Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("unconfigured embedder must error")
	}
}

func TestOpenAIEmbedder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer sk-embed" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.5]},{"index":1,"embedding":[0.25]}]}`))
	}))
	defer server.Close()

	embedder := NewOpenAIEmbedder(server.URL+"/v1", "bge-m3", "sk-embed", server.Client())
	vectors, err := embedder.Embed(context.Background(), []string{"một", "hai"})
	if err != nil {
		t.Fatalf("embed failed: %v", err)
	}
	if len(vectors) != 2 || vectors[0][0] != 0.5 || vectors[1][0] != 0.25 {
		t.Fatalf("vectors wrong: %v", vectors)
	}
}

func TestChunk(t *testing.T) {
	markdown := `# Sản phẩm A
Giới thiệu tổng quan.

## Cài đặt
Bước 1. Cài agent.
Bước 2. Chạy agent.

## Cài đặt lại
Làm lại từ đầu.
`
	title, chunks := Chunk(markdown)
	if title != "Sản phẩm A" {
		t.Fatalf("title wrong: %q", title)
	}
	// Intro text before the first heading becomes a headingless chunk so it
	// still gets indexed.
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "" || !strings.Contains(chunks[0].Content, "Giới thiệu") {
		t.Fatalf("intro chunk wrong: %+v", chunks[0])
	}
	if chunks[1].Heading != "Cài đặt" || !strings.Contains(chunks[1].Content, "Bước 1") {
		t.Fatalf("chunk 1 wrong: %+v", chunks[1])
	}
	if chunks[2].Heading != "Cài đặt lại" {
		t.Fatalf("chunk 2 wrong: %+v", chunks[2])
	}
}

func TestSplitOversizedKeepsParagraphs(t *testing.T) {
	body := strings.Repeat("đoạn văn.", 10) + "\n\n" + strings.Repeat("khác.", 10)
	parts := splitOversized(body, 5)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	for _, part := range parts {
		if strings.Contains(part, "\n\n\n") {
			t.Fatalf("paragraph integrity broken: %q", part)
		}
	}
}
