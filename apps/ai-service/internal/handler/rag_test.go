package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

func TestRAGHandlerPreviewChunks(t *testing.T) {
	svc := knowledge.NewService(nil, nil, nil)
	ragHandler := NewRAGHandler(svc)

	mux := http.NewServeMux()
	ragHandler.RegisterRoutes(mux)

	body, _ := json.Marshal(knowledge.ChunkPreviewRequest{
		Content: "## Tiêu đề\n\nNội dung văn bản kiểm tra.",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rag/sources/preview-chunks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res knowledge.ChunkPreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.TotalChunks != 1 {
		t.Errorf("expected 1 chunk, got %d", res.TotalChunks)
	}
	if len(res.Chunks) != 1 || res.Chunks[0].Heading != "Tiêu đề" {
		t.Errorf("unexpected chunks: %+v", res.Chunks)
	}
}

func TestRAGHandlerParsePreview(t *testing.T) {
	svc := knowledge.NewService(nil, nil, nil)
	ragHandler := NewRAGHandler(svc)

	mux := http.NewServeMux()
	ragHandler.RegisterRoutes(mux)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "policy.md")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = part.Write([]byte("## Quy chế nghỉ phép\n\nMỗi năm được 12 ngày phép."))
	_ = writer.WriteField("chunk_size", "512")
	_ = writer.WriteField("chunk_overlap", "64")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/rag/sources/parse-preview", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res knowledge.ChunkPreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.TotalChunks != 1 {
		t.Errorf("expected 1 chunk, got %d", res.TotalChunks)
	}
	if len(res.Chunks) != 1 || res.Chunks[0].Heading != "Quy chế nghỉ phép" {
		t.Errorf("unexpected chunks: %+v", res.Chunks)
	}
}
