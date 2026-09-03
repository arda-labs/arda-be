package svcclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// RAGHit is one retrieved knowledge chunk. SourceID/SourceVersionID are the
// ai_knowledge_sources / ai_knowledge_chunks row ids (ints in the service
// contract, matching apps/rag-service/app/domain/models.py QueryHitOut).
type RAGHit struct {
	SourceID        int     `json:"source_id"`
	SourceVersionID int     `json:"source_version_id"`
	Version         string  `json:"version"`
	Title           string  `json:"title"`
	Heading         string  `json:"heading"`
	Content         string  `json:"content"`
	Score           float64 `json:"score"`
	Citation        string  `json:"citation"`
}

// RAGResponse is the full POST /api/rag/query response.
type RAGResponse struct {
	RunID          string   `json:"run_id"`
	Hits           []RAGHit `json:"hits"`
	LatencyMs      int      `json:"latency_ms"`
	Rewritten      bool     `json:"rewritten"`
	RetrievedCount int      `json:"retrieved_count"`
	RerankedCount  int      `json:"reranked_count"`
}

// RAGQueryRequest is the POST /api/rag/query body — identity fields never
// travel here, only the query and top-k bound.
type RAGQueryRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

// RAGClient calls the rag-service knowledge retrieval surface
// (/api/rag/query). POST is a mutation from the transport's point of view, so
// it is never auto-retried.
type RAGClient struct {
	*Client
}

// NewRAGClient returns a typed client for the rag-service.
func NewRAGClient(baseURL, source, secret string, hc *http.Client) *RAGClient {
	return &RAGClient{Client: NewClient("rag-service", baseURL, source, secret, hc)}
}

// Search retrieves the top-k knowledge chunks for query in the delegated
// tenant/org scope.
func (c *RAGClient) Search(ctx context.Context, md metadata.Context, query string, topK int) (*RAGResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 512 {
		return nil, fmt.Errorf("query is required (max 512 characters)")
	}
	req, err := c.NewRequest(ctx, http.MethodPost, "/api/rag/query", md)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(RAGQueryRequest{Query: query, TopK: topK}); err != nil {
		return nil, fmt.Errorf("encode rag query: %w", err)
	}
	req.Body = io.NopCloser(&buf)
	req.ContentLength = int64(buf.Len())
	req.Method = http.MethodPost // override the GET-only default from NewRequest
	req.Header.Set("Content-Type", "application/json") // FastAPI requires it (422 without)

	var result RAGResponse
	if err := c.Do(ctx, req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
