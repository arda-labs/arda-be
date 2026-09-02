package catalog

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

type fixedSearcher struct{}

func (fixedSearcher) Search(context.Context, string, string, int) ([]knowledge.Hit, error) {
	return []knowledge.Hit{
		{
			SourceID:   "src-1",
			SourceKey:  "docs/faq.md",
			Title:      "FAQ hệ thống",
			Version:    "v1",
			Heading:    "Cách tra FAQ",
			Content:    "Nội dung FAQ...",
			SourceType: "faq",
		},
	}, nil
}

// TestKnowledgeSearchResultShape guards the Goja contract: the dispatcher
// must return the KnowledgeSearchResult shape promised by the JSDoc
// (sourceTitle/content/...), not the Go struct's field names — Goja ignores
// JSON tags, so a []Hit return surfaces as Title/Content to the model.
func TestKnowledgeSearchResultShape(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterKnowledgeCatalog(reg, fixedSearcher{})

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
	for _, key := range []string{"sourceId", "sourceTitle", "content", "citations", "matchScore"} {
		if _, present := shaped[key]; !present {
			t.Errorf("contract field %q missing from result (got keys %v)", key, shaped)
		}
	}
	if shaped["sourceTitle"] != "FAQ hệ thống" {
		t.Errorf("sourceTitle = %v, want FAQ hệ thống", shaped["sourceTitle"])
	}
	if shaped["content"] != "Nội dung FAQ..." {
		t.Errorf("content = %v, want Nội dung FAQ...", shaped["content"])
	}
}

// TestKnowledgeSearchRejectsBlankQuery covers the argument guard.
func TestKnowledgeSearchRejectsBlankQuery(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterKnowledgeCatalog(reg, fixedSearcher{})
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
