package catalog

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// fakeRAG implements ragSearcher for unit tests. It captures the arguments
// so callers can assert search dispatch.
type fakeRAG struct {
	capturedQuery string
	capturedTopK  int
}

func (f *fakeRAG) Search(ctx context.Context, md metadata.Context, query string, topK int) (*svcclient.RAGResponse, error) {
	f.capturedQuery = query
	f.capturedTopK = topK
	return &svcclient.RAGResponse{
		RunID: "test-run-1",
		Hits: []svcclient.RAGHit{
			{SourceID: 7, SourceVersionID: 1, Version: "v1", Title: "FAQ hệ thống", Heading: "Cách tra FAQ", Content: "Nội dung FAQ...", Score: 0.42, Citation: "[7:Cách tra FAQ]"},
		},
	}, nil
}

// TestKnowledgeSearchResultShape guards the Goja contract: the dispatcher
// must return the KnowledgeSearchResult shape promised by the JSDoc
// (sourceId, sourceTitle, content, citations, matchScore).
func TestKnowledgeSearchResultShape(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterBuiltinCatalog(reg, &fakeRAG{})

	fn, entry, ok := reg.Resolve("knowledge.search")
	if !ok {
		t.Fatal("knowledge.search not registered")
	}

	scope := iamScope()
	scope.Permissions["ai.knowledge.read"] = struct{}{}
	if err := entry.CheckPermissions(scope); err != nil {
		t.Fatalf("expected permission granted, got %v", err)
	}
	result, err := fn(context.Background(), scope, map[string]any{"query": "FAQ"})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	items, ok := result.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", result)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(items))
	}

	raw, err := json.Marshal(items[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var shaped map[string]any
	if err := json.Unmarshal(raw, &shaped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"runId", "sourceId", "sourceTitle", "content", "citations", "matchScore"} {
		if _, present := shaped[key]; !present {
			t.Errorf("contract field %q missing from result (got keys %v)", key, shaped)
		}
	}
	if shaped["runId"] != "test-run-1" {
		t.Errorf("runId = %v, want test-run-1", shaped["runId"])
	}
	// sourceId should be a float64 in JSON (Go's default JSON number).
	if v, ok := shaped["sourceId"].(float64); !ok || int(v) != 7 {
		t.Errorf("sourceId = %v (type %T), want 7", shaped["sourceId"], shaped["sourceId"])
	}
	if shaped["sourceTitle"] != "FAQ hệ thống" {
		t.Errorf("sourceTitle = %v, want FAQ hệ thống", shaped["sourceTitle"])
	}
	if shaped["content"] != "Nội dung FAQ..." {
		t.Errorf("content = %v, want Nội dung FAQ...", shaped["content"])
	}
	if shaped["citations"] != "[7:Cách tra FAQ]" {
		t.Errorf("citations = %v, want [7:Cách tra FAQ]", shaped["citations"])
	}
	if v, ok := shaped["matchScore"].(float64); !ok || v != 0.42 {
		t.Errorf("matchScore = %v (type %T), want 0.42", shaped["matchScore"], shaped["matchScore"])
	}
}

// TestKnowledgeSearchRejectsBlankQuery covers the argument guard.
func TestKnowledgeSearchRejectsBlankQuery(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterBuiltinCatalog(reg, &fakeRAG{})
	fn, _, ok := reg.Resolve("knowledge.search")
	if !ok {
		t.Fatal("knowledge.search not registered")
	}
	if _, err := fn(context.Background(), iamScope(), map[string]any{"query": "   "}); err == nil {
		t.Fatal("expected blank query to be rejected")
	}
	if _, err := fn(context.Background(), iamScope(), map[string]any{}); err == nil {
		t.Fatal("expected missing query to be rejected")
	}
}

// TestKnowledgeSearchSendsTopK asserts that the limit argument is forwarded
// to the RAG client as topK.
func TestKnowledgeSearchSendsTopK(t *testing.T) {
	fake := &fakeRAG{}
	reg := NewDispatcherRegistry()
	RegisterBuiltinCatalog(reg, fake)
	fn, _, ok := reg.Resolve("knowledge.search")
	if !ok {
		t.Fatal("knowledge.search not registered")
	}
	if _, err := fn(context.Background(), iamScope(), map[string]any{"query": "test", "limit": float64(2)}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if fake.capturedTopK != 2 {
		t.Errorf("expected topK=2, got %d", fake.capturedTopK)
	}
	if fake.capturedQuery != "test" {
		t.Errorf("expected query=test, got %q", fake.capturedQuery)
	}
}
