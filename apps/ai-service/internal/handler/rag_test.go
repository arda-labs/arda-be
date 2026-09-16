package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

func TestRAGHandlerRequiresGatewayIdentity(t *testing.T) {
	svc := knowledge.NewService(nil, nil, nil)
	mux := http.NewServeMux()
	NewRAGHandler(svc).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/rag/query", bytes.NewBufferString(`{"query":"policy"}`))
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d: %s", res.Code, res.Body.String())
	}
}

// TestRAGQueryRequiresKnowledgeRead locks the permission fix: the standalone
// endpoint serves the same knowledge chunks as the knowledge.search tool, so
// ai.assistant.use alone must not be enough.
func TestRAGQueryRequiresKnowledgeRead(t *testing.T) {
	svc := knowledge.NewService(nil, nil, nil)
	mux := http.NewServeMux()
	NewRAGHandler(svc).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/rag/query", bytes.NewBufferString(`{"query":"policy"}`))
	setAIIdentityHeaders(req) // X-Permissions: ai.assistant.use only
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d: %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "ai.knowledge_forbidden") {
		t.Fatalf("expected ai.knowledge_forbidden, got %s", res.Body.String())
	}
}

// TestRAGQueryAllowsKnowledgeRead verifies the request passes the permission
// gate when ai.knowledge.read is present. The empty query fails later
// validation (400) instead of authorization (403), which keeps the test free
// of database dependencies.
func TestRAGQueryAllowsKnowledgeRead(t *testing.T) {
	svc := knowledge.NewService(nil, nil, nil)
	mux := http.NewServeMux()
	NewRAGHandler(svc).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/rag/query", bytes.NewBufferString(`{"query":""}`))
	setAIIdentityHeaders(req)
	req.Header.Set("X-Permissions", "ai.assistant.use,ai.knowledge.read")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code == http.StatusForbidden {
		t.Fatalf("permission should pass, got 403: %s", res.Body.String())
	}
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "rag.invalid_query") {
		t.Fatalf("expected rag.invalid_query 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestRAGFeedbackRequiresKnowledgeRead(t *testing.T) {
	svc := knowledge.NewService(nil, nil, nil)
	mux := http.NewServeMux()
	NewRAGHandler(svc).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/rag/feedback", bytes.NewBufferString(`{"run_id":"r1","helpful":true}`))
	setAIIdentityHeaders(req)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), "ai.knowledge_forbidden") {
		t.Fatalf("expected ai.knowledge_forbidden 403, got %d: %s", res.Code, res.Body.String())
	}
}

func TestRAGFeedbackAllowsKnowledgeRead(t *testing.T) {
	svc := knowledge.NewService(nil, nil, nil)
	mux := http.NewServeMux()
	NewRAGHandler(svc).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/rag/feedback", bytes.NewBufferString(`not-json`))
	setAIIdentityHeaders(req)
	req.Header.Set("X-Permissions", "ai.assistant.use,ai.knowledge.read")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "rag.invalid_json") {
		t.Fatalf("expected rag.invalid_json 400, got %d: %s", res.Code, res.Body.String())
	}
}

func TestHasRequestPermission(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{"tenant permission", map[string]string{"X-Permissions": "ai.assistant.use,ai.knowledge.read"}, true},
		{"global permission", map[string]string{"X-Global-Permissions": "ai.knowledge.read"}, true},
		{"superadmin wildcard", map[string]string{"X-Permissions": "superadmin"}, true},
		{"global admin bypass", map[string]string{"X-Global-Admin": "true"}, true},
		{"assistant only", map[string]string{"X-Permissions": "ai.assistant.use"}, false},
		{"no headers", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/rag/query", nil)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			if got := hasRequestPermission(req, knowledgeReadPermission); got != tc.want {
				t.Fatalf("hasRequestPermission = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRAGHandlerPreviewChunks(t *testing.T) {
	svc := knowledge.NewService(nil, nil, nil)
	ragHandler := NewRAGHandler(svc)

	mux := http.NewServeMux()
	ragHandler.RegisterRoutes(mux)

	body, _ := json.Marshal(knowledge.ChunkPreviewRequest{
		Content: "## Tiêu đề\n\nNội dung văn bản kiểm tra.",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/rag/sources/preview-chunks", bytes.NewReader(body))
	setAIIdentityHeaders(req)
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
	setAIIdentityHeaders(req)
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
