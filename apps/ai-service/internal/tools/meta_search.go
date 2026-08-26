package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type searchArguments struct {
	Query  string `json:"query"`
	Domain string `json:"domain,omitempty"`
}

type SearchMetaTool struct {
	searchFn func(query, domain string, scope Context) (signatures string, count int, err error)
}

func NewSearchMetaTool(searchFn func(query, domain string, scope Context) (signatures string, count int, err error)) *SearchMetaTool {
	return &SearchMetaTool{searchFn: searchFn}
}

func (t *SearchMetaTool) Definition() Definition {
	return Definition{
		Name:        "search",
		Version:     1,
		Kind:        "read",
		Description: "Search the arda.* TypeScript SDK catalog for available method signatures and JSDoc matching a query or domain filter.",
		Risk:        "low",
		Timeout:     2 * time.Second,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {
					"type": "string",
					"maxLength": 256,
					"description": "Natural language keywords describing the action or data needed (e.g. 'customer risk profile', 'search knowledge', 'export csv')"
				},
				"domain": {
					"type": "string",
					"description": "Optional domain filter (e.g. 'crm', 'knowledge', 'hrm', 'finance', 'workflow')"
				}
			},
			"required": ["query"]
		}`),
	}
}

func (t *SearchMetaTool) Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error) {
	if t.searchFn == nil {
		return Result{}, fmt.Errorf("SDK catalog search function is not configured")
	}

	var input searchArguments
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Query) == "" {
		return Result{}, fmt.Errorf("%w: query is required", ErrInvalidArgument)
	}

	signatures, count, err := t.searchFn(input.Query, input.Domain, scope)
	if err != nil {
		return Result{}, err
	}

	resPayload := map[string]any{
		"signatures": signatures,
		"count":      count,
		"query":      input.Query,
	}

	data, err := json.Marshal(resPayload)
	if err != nil {
		return Result{}, fmt.Errorf("encode search results: %w", err)
	}

	summary := fmt.Sprintf("Found %d SDK methods matching '%s'.", count, input.Query)
	return Result{
		Data:      data,
		Summary:   summary,
		Source:    "ai-sdk-catalog",
		RequestID: scope.RequestID,
		FreshAt:   time.Now().UTC(),
	}, nil
}
