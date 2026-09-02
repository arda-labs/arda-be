package catalog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// RegisterKnowledgeCatalog registers knowledge SDK methods (arda.knowledge.*).
func RegisterKnowledgeCatalog(reg *DispatcherRegistry, searcher knowledge.Searcher) {
	if searcher == nil {
		return
	}

	// arda.knowledge.search (Read)
	reg.Register(
		CatalogEntry{
			MethodName: "knowledge.search",
			SDKPath:    "arda.knowledge.search",
			Domain:     "knowledge",
			Signature:  "arda.knowledge.search(args: { query: string; limit?: number }): Promise<KnowledgeSearchResult[]>;",
			JSDoc: `/**
 * Search published knowledge sources and business documentation with citations.
 * @param args.query Natural language search query (max 512 chars)
 * @param args.limit Number of items, 1-5 (default 3)
 * @returns KnowledgeSearchResult[] { sourceId, sourceTitle, content, citations, matchScore }
 * @requires ai.knowledge.read
 * @domain knowledge
 */`,
			Keywords:            []string{"knowledge", "doc", "faq", "policy", "procedure", "search", "rag", "guide", "rule", "tài liệu", "nội bộ", "chính sách", "quy trình", "nghỉ phép"},
			Kind:                "read",
			RequiredPermissions: []string{"ai.knowledge.read"},
			Risk:                "low",
			Timeout:             3 * time.Second,
		},
		func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
			query, _ := args["query"].(string)
			query = strings.TrimSpace(query)
			if query == "" || len(query) > 512 {
				return nil, fmt.Errorf("query is required (max 512 characters)")
			}

			limit := 3
			if l, ok := args["limit"].(float64); ok && l > 0 && l <= 5 {
				limit = int(l)
			}

			items, err := searcher.Search(ctx, scope.TenantID, query, limit)
			if err != nil {
				return nil, fmt.Errorf("knowledge search error: %w", err)
			}
			return items, nil
		},
	)
}
