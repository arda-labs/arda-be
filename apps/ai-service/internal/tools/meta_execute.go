package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SandboxExecutor interface {
	Execute(ctx context.Context, scope Context, code string) (any, error)
}

type executeArguments struct {
	Code string `json:"code"`
}

type ExecuteMetaTool struct {
	executeFn func(ctx context.Context, scope Context, code string) (map[string]any, error)
}

func NewExecuteMetaTool(executeFn func(ctx context.Context, scope Context, code string) (map[string]any, error)) *ExecuteMetaTool {
	return &ExecuteMetaTool{executeFn: executeFn}
}

func (t *ExecuteMetaTool) Definition() Definition {
	return Definition{
		Name:        "execute",
		Version:     1,
		Kind:        "read",
		Description: "Execute sandboxed JavaScript code against the arda.* SDK to query, aggregate, filter, or propose actions across Arda domain services.",
		Risk:        "low",
		Timeout:     4 * time.Second,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"code": {
					"type": "string",
					"description": "JavaScript (ES6) code to execute. Use await with arda.* SDK methods (e.g. arda.crm.getCustomer), array transformations (map, filter, reduce), and return the final computed result."
				}
			},
			"required": ["code"]
		}`),
	}
}

func (t *ExecuteMetaTool) Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error) {
	if t.executeFn == nil {
		return Result{}, fmt.Errorf("sandbox executor is not configured")
	}

	var input executeArguments
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Code) == "" {
		return Result{}, fmt.Errorf("%w: code is required", ErrInvalidArgument)
	}

	execResult, err := t.executeFn(ctx, scope, input.Code)
	if err != nil {
		// Even on error, return structured error details in Result so model can inspect or rephrase
		data, marshalErr := json.Marshal(map[string]any{
			"error": err.Error(),
		})
		if marshalErr == nil {
			return Result{
				Data:      data,
				Summary:   fmt.Sprintf("Sandbox execution error: %s", err.Error()),
				Source:    "ai-sandbox",
				RequestID: scope.RequestID,
				FreshAt:   time.Now().UTC(),
			}, nil
		}
		return Result{}, err
	}

	data, err := json.Marshal(execResult)
	if err != nil {
		return Result{}, fmt.Errorf("encode sandbox execution result: %w", err)
	}

	summary := "Executed sandboxed script successfully."
	if duration, ok := execResult["durationMs"]; ok {
		summary = fmt.Sprintf("Executed sandboxed script in %vms.", duration)
	}

	return Result{
		Data:      data,
		Summary:   summary,
		Source:    "ai-sandbox",
		RequestID: scope.RequestID,
		FreshAt:   time.Now().UTC(),
	}, nil
}
