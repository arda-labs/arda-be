package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type readResultArguments struct {
	ResultID string `json:"resultId"`
}

// ReadResultMetaTool exposes the readResult tool: given a resultId from a
// previous execute() call, it returns the full sandbox output the model chose
// not to receive inline (raw results stay in the sandbox store — the
// Anthropic/Cloudflare "filesystem as context" pattern).
type ReadResultMetaTool struct {
	readFn func(ctx context.Context, scope Context, resultID string) (map[string]any, error)
}

func NewReadResultMetaTool(readFn func(ctx context.Context, scope Context, resultID string) (map[string]any, error)) *ReadResultMetaTool {
	return &ReadResultMetaTool{readFn: readFn}
}

func (t *ReadResultMetaTool) Definition() Definition {
	return Definition{
		Name:        "readResult",
		Version:     1,
		Kind:        "read",
		Description: "Read the full output of a previous execute() call by its resultId. Use this when execute() returned a resultId but you need the complete data (e.g. more rows, full object) to continue.",
		Risk:        "low",
		Timeout:     2 * time.Second,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"resultId": {
					"type": "string",
					"description": "The resultId returned by a previous execute() call."
				}
			},
			"required": ["resultId"]
		}`),
	}
}

func (t *ReadResultMetaTool) Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error) {
	if t.readFn == nil {
		return Result{}, fmt.Errorf("result reader is not configured")
	}

	var input readResultArguments
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.ResultID) == "" {
		return Result{}, fmt.Errorf("%w: resultId is required", ErrInvalidArgument)
	}

	data, err := t.readFn(ctx, scope, strings.TrimSpace(input.ResultID))
	if err != nil {
		return Result{
			Data:      json.RawMessage(fmt.Sprintf(`{"error":%q}`, err.Error())),
			Summary:   fmt.Sprintf("Result lookup failed: %s", err.Error()),
			Source:    "ai-sandbox",
			RequestID: scope.RequestID,
			FreshAt:   time.Now().UTC(),
		}, nil
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return Result{}, fmt.Errorf("encode result: %w", err)
	}
	return Result{
		Data:      raw,
		Summary:   "Returned the full stored execution result.",
		Source:    "ai-sandbox",
		RequestID: scope.RequestID,
		FreshAt:   time.Now().UTC(),
	}, nil
}
