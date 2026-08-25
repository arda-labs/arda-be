package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

type knowledgeSearchArguments struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type KnowledgeSearchTool struct {
	searcher knowledge.Searcher
}

func NewKnowledgeSearchTool(searcher knowledge.Searcher) *KnowledgeSearchTool {
	return &KnowledgeSearchTool{searcher: searcher}
}

func (t *KnowledgeSearchTool) Definition() Definition {
	return Definition{
		Name:                "knowledge.search",
		Version:             1,
		Kind:                "read",
		Description:         "Search published tenant or global Arda knowledge and return citations",
		RequiredPermissions: []string{"ai.knowledge.read"},
		Risk:                "low",
		Timeout:             3 * time.Second,
		RedactionProfile:    "knowledge_citation",
	}
}

func (t *KnowledgeSearchTool) Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error) {
	var input knowledgeSearchArguments
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Query) == "" || len(input.Query) > 512 {
		return Result{}, fmt.Errorf("%w: query is required and must be at most 512 characters", ErrInvalidArgument)
	}
	if input.Limit == 0 {
		input.Limit = 5
	}
	if input.Limit < 1 || input.Limit > 5 {
		return Result{}, fmt.Errorf("%w: limit must be between 1 and 5", ErrInvalidArgument)
	}
	if t == nil || t.searcher == nil {
		return Result{}, fmt.Errorf("knowledge search is not configured")
	}
	hits, err := t.searcher.Search(ctx, scope.TenantID, input.Query, input.Limit)
	if err != nil {
		return Result{}, err
	}
	data, err := json.Marshal(map[string]any{
		"query":     input.Query,
		"items":     hits,
		"citations": citations(hits),
	})
	if err != nil {
		return Result{}, fmt.Errorf("encode knowledge result: %w", err)
	}
	return Result{
		Data: data, Summary: fmt.Sprintf("I found %d approved knowledge result(s).", len(hits)),
		Source: "arda-ai-knowledge", RequestID: scope.RequestID, FreshAt: time.Now().UTC(),
	}, nil
}

func citations(hits []knowledge.Hit) []map[string]string {
	items := make([]map[string]string, 0, len(hits))
	for _, hit := range hits {
		items = append(items, map[string]string{
			"sourceId": hit.SourceID, "sourceKey": hit.SourceKey, "title": hit.Title,
			"version": hit.Version, "heading": hit.Heading,
		})
	}
	return items
}
