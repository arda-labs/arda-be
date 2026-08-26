package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSearchMetaTool_Execution(t *testing.T) {
	searchTool := NewSearchMetaTool(func(query, domain string, scope Context) (string, int, error) {
		if query == "crm customer" {
			return "arda.crm.getCustomer(args: { customerId: string }): Promise<CustomerSummary>;", 1, nil
		}
		return "", 0, nil
	})

	ctx := context.Background()
	scope := Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
	}

	// Valid search
	res, err := searchTool.Execute(ctx, scope, json.RawMessage(`{"query":"crm customer"}`))
	if err != nil {
		t.Fatalf("unexpected search error: %v", err)
	}

	if !strings.Contains(string(res.Data), "arda.crm.getCustomer") {
		t.Errorf("expected search result to contain arda.crm.getCustomer, got: %s", string(res.Data))
	}

	// Invalid empty query
	_, err = searchTool.Execute(ctx, scope, json.RawMessage(`{"query":""}`))
	if err == nil {
		t.Fatal("expected error on empty query, got nil")
	}
}

func TestExecuteMetaTool_Execution(t *testing.T) {
	executeTool := NewExecuteMetaTool(func(ctx context.Context, scope Context, code string) (map[string]any, error) {
		return map[string]any{
			"output":     map[string]any{"customerName": "Acme"},
			"durationMs": 42,
		}, nil
	})

	ctx := context.Background()
	scope := Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
	}

	res, err := executeTool.Execute(ctx, scope, json.RawMessage(`{"code":"return { customerName: 'Acme' };"}`))
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	if !strings.Contains(string(res.Data), "Acme") {
		t.Errorf("expected execute result to contain Acme, got: %s", string(res.Data))
	}

	// Invalid empty code
	_, err = executeTool.Execute(ctx, scope, json.RawMessage(`{"code":""}`))
	if err == nil {
		t.Fatal("expected error on empty code, got nil")
	}
}

func TestRegistry_PermissionsAndExecutionOnly(t *testing.T) {
	searchTool := NewSearchMetaTool(func(query, domain string, scope Context) (string, int, error) {
		return "sig", 1, nil
	})
	executeTool := NewExecuteMetaTool(func(ctx context.Context, scope Context, code string) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})

	reg := NewRegistry(searchTool, executeTool)

	// Only 2 tools in Definitions (for LLM)
	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("expected exactly 2 tools in Definitions, got %d", len(defs))
	}

	scope := Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
	}

	// Resolve search
	tool, def, err := reg.Resolve(Call{Name: "search", Version: 1}, scope)
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if def.Name != "search" || tool == nil {
		t.Errorf("expected search tool, got %v", def.Name)
	}

	// Resolve execute
	tool, def, err = reg.Resolve(Call{Name: "execute", Version: 1}, scope)
	if err != nil {
		t.Fatalf("unexpected resolve error: %v", err)
	}
	if def.Name != "execute" || tool == nil {
		t.Errorf("expected execute tool, got %v", def.Name)
	}

	// Unknown tool
	_, _, err = reg.Resolve(Call{Name: "unknown", Version: 1}, scope)
	if err != ErrUnknownTool {
		t.Errorf("expected ErrUnknownTool, got %v", err)
	}
}
