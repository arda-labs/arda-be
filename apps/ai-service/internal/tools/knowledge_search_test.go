package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

type fakeKnowledgeSearcher struct {
	tenant string
	query  string
}

func (s *fakeKnowledgeSearcher) Search(_ context.Context, tenantID, query string, _ int) ([]knowledge.Hit, error) {
	s.tenant, s.query = tenantID, query
	return []knowledge.Hit{{SourceID: "source-1", SourceKey: "runbook/one", Title: "One", Version: "1", Heading: "Heading", Content: "Approved content", SourceType: "procedure"}}, nil
}

func TestKnowledgeSearchReturnsCitationsAndTenantScope(t *testing.T) {
	searcher := &fakeKnowledgeSearcher{}
	tool := NewKnowledgeSearchTool(searcher)
	result, err := tool.Execute(context.Background(), Context{TenantID: "tenant-1"}, json.RawMessage(`{"query":"approved","limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if searcher.tenant != "tenant-1" || searcher.query != "approved" {
		t.Fatalf("search scope = %q/%q", searcher.tenant, searcher.query)
	}
	if result.Summary != "I found 1 approved knowledge result(s)." || len(result.Data) == 0 {
		t.Fatalf("result = %#v", result)
	}
}
