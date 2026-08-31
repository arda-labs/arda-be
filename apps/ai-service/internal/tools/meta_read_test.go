package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestReadResultMetaTool_Execution(t *testing.T) {
	readTool := NewReadResultMetaTool(func(ctx context.Context, scope Context, resultID string) (map[string]any, error) {
		if resultID != "run-1:1" {
			t.Errorf("unexpected resultId %q", resultID)
		}
		return map[string]any{"output": []map[string]any{{"id": "c1"}}, "scriptHash": "abc"}, nil
	})

	args := json.RawMessage(`{"resultId":"run-1:1"}`)
	res, err := readTool.Execute(context.Background(), Context{TenantID: "t", ActorUserID: "u", RequestID: "run-1"}, args)
	if err != nil {
		t.Fatalf("readResult failed: %v", err)
	}
	if !json.Valid(res.Data) {
		t.Errorf("invalid JSON data: %s", res.Data)
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Data, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := payload["output"]; !ok {
		t.Error("expected output field")
	}
	if res.Summary == "" {
		t.Error("expected summary")
	}
}

func TestReadResultMetaTool_MissingResultID(t *testing.T) {
	readTool := NewReadResultMetaTool(func(ctx context.Context, scope Context, resultID string) (map[string]any, error) {
		return nil, nil
	})

	for _, args := range []string{`{}`, `{"resultId":""}`, `{"resultId":"run-1:1","extra":1}`} {
		if _, err := readTool.Execute(context.Background(), Context{}, json.RawMessage(args)); err == nil {
			t.Errorf("args %s: expected error", args)
		}
	}
}

func TestReadResultMetaTool_NotFound(t *testing.T) {
	readTool := NewReadResultMetaTool(func(ctx context.Context, scope Context, resultID string) (map[string]any, error) {
		return nil, ErrUnknownTool // any error → structured result, no tool error
	})
	res, err := readTool.Execute(context.Background(), Context{}, json.RawMessage(`{"resultId":"run-1:99"}`))
	if err != nil {
		t.Fatalf("expected structured result, got error: %v", err)
	}
	if !json.Valid(res.Data) {
		t.Errorf("invalid JSON: %s", res.Data)
	}
}

func TestReadResultMetaTool_Definition(t *testing.T) {
	readTool := NewReadResultMetaTool(func(ctx context.Context, scope Context, resultID string) (map[string]any, error) {
		return nil, nil
	})
	def := readTool.Definition()
	if def.Name != "readResult" || def.Kind != "read" {
		t.Errorf("unexpected definition: %+v", def)
	}
	if len(def.RequiredPermissions) != 0 {
		t.Errorf("readResult should not require extra permissions, got %v", def.RequiredPermissions)
	}
}
