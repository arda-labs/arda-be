package catalog

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
	"github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

// RAGSearcher is the narrow interface consumed by the knowledge.search
// handler. RAGClient or in-process RAG adapter satisfies it structurally.
type RAGSearcher interface {
	Search(ctx context.Context, md metadata.Context, query string, topK int) (*svcclient.RAGResponse, error)
}

type ragSearcher = RAGSearcher

// RegisterKnowledgeCatalog registers knowledge SDK methods (arda.knowledge.*).
func RegisterKnowledgeCatalog(reg *DispatcherRegistry, rag ragSearcher) {
	if rag == nil {
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
	 * @returns KnowledgeSearchResult[] { runId, sourceId, sourceTitle, content, citations, matchScore }
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

			resp, err := rag.Search(ctx, scopeToMetadata(scope), query, limit)
			if err != nil {
				return nil, fmt.Errorf("knowledge search error: %w", err)
			}
			return knowledgeResults(resp), nil
		},
	)
}

// knowledgeResults projects a svcclient.RAGResponse onto the
// KnowledgeSearchResult shape promised by the JSDoc contract. runId lets the
// UI attach feedback to the exact RAG run that produced the hits.
func knowledgeResults(resp *svcclient.RAGResponse) []map[string]any {
	results := make([]map[string]any, 0, len(resp.Hits))
	for _, hit := range resp.Hits {
		results = append(results, map[string]any{
			"runId":       resp.RunID,
			"sourceId":    hit.SourceID,
			"version":     hit.Version,
			"heading":     hit.Heading,
			"sourceTitle": hit.Title,
			"content":     hit.Content,
			"citations":   hit.Citation,
			"matchScore":  hit.Score,
		})
	}
	return results
}
