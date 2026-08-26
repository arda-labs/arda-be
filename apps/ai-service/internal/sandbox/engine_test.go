package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

type mockRegistry struct {
	methods []SDKMethod
}

func (m *mockRegistry) AllSDKMethods() []SDKMethod {
	return m.methods
}

func setupTestEngine() (*Engine, *mockRegistry) {
	reg := &mockRegistry{
		methods: []SDKMethod{
			{
				MethodName: "crm.getCustomer",
				SDKPath:    "arda.crm.getCustomer",
				Domain:     "crm",
				Timeout:    time.Second,
				CheckPermissions: func(scope tools.Context) error {
					if _, ok := scope.Permissions["crm.customer.read"]; !ok {
						return tools.ErrToolForbidden
					}
					return nil
				},
				Dispatcher: func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
					id, _ := args["customerId"].(string)
					return map[string]any{
						"id":           id,
						"customerCode": "CUST-001",
						"name":         "Acme Corp",
						"status":       "ACTIVE",
						"riskLevel":    "low",
					}, nil
				},
			},
			{
				MethodName: "crm.exportCustomer",
				SDKPath:    "arda.crm.exportCustomer",
				Domain:     "crm",
				Timeout:    time.Second,
				CheckPermissions: func(scope tools.Context) error {
					if _, ok := scope.Permissions["crm.customer.export"]; !ok {
						return tools.ErrToolForbidden
					}
					return nil
				},
				Dispatcher: func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
					return nil, tools.ErrApprovalRequired
				},
			},
		},
	}

	return NewEngine(reg), reg
}

func testScope() tools.Context {
	return tools.Context{
		TenantID:    "tenant-test",
		ActorUserID: "user-test",
		Permissions: map[string]struct{}{
			"crm.customer.read":   {},
			"crm.customer.export": {},
		},
	}
}

func TestStaticValidator_BlocksDangerousTokens(t *testing.T) {
	dangerous := []string{
		"eval('1+1')",
		"const f = new Function('return 1');",
		"const p = ({}).__proto__;",
		"globalThis.evil = true;",
		"process.exit(1);",
		"require('fs')",
		"import('something')",
		"window.location = 'evil';",
		"setTimeout(() => {}, 100);",
	}

	for _, code := range dangerous {
		err := ValidateScript(code)
		if err == nil {
			t.Errorf("expected ValidateScript to reject '%s', but got nil", code)
		}
	}
}

func TestEngine_ExecutesValidScript(t *testing.T) {
	engine, _ := setupTestEngine()
	ctx := context.Background()
	scope := testScope()

	code := `
		console.log("fetching customer 123");
		const res = await arda.crm.getCustomer({ customerId: "123" });
		console.log("customer received:", res.name);
		return {
			customerName: res.name,
			code: res.customerCode
		};
	`

	result, err := engine.Execute(ctx, scope, code)
	if err != nil {
		t.Fatalf("unexpected error executing script: %v", err)
	}

	outMap, ok := result.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any output, got %T: %v", result.Output, result.Output)
	}

	if outMap["customerName"] != "Acme Corp" {
		t.Errorf("expected customerName 'Acme Corp', got '%v'", outMap["customerName"])
	}
	if len(result.MethodsCalled) != 1 || result.MethodsCalled[0] != "arda.crm.getCustomer" {
		t.Errorf("expected MethodsCalled ['arda.crm.getCustomer'], got %v", result.MethodsCalled)
	}

	// Verify console.log capture
	if len(result.Logs) != 2 {
		t.Errorf("expected 2 console logs, got %d: %v", len(result.Logs), result.Logs)
	}
	if len(result.Logs) > 0 && !strings.Contains(result.Logs[0], "fetching customer 123") {
		t.Errorf("unexpected log content: %s", result.Logs[0])
	}
}

func TestEngine_EnforcesTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	engine, _ := setupTestEngine()
	ctx := context.Background()
	scope := testScope()

	code := `
		var i = 0;
		while (true) {
			i++;
		}
	`

	start := time.Now()
	_, err := engine.Execute(ctx, scope, code)
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected infinite loop to return error, got nil")
	}
	if !strings.Contains(err.Error(), "ai.sandbox_timeout") {
		t.Errorf("expected sandbox timeout error, got: %v", err)
	}
	if duration > 4000*time.Millisecond {
		t.Errorf("expected interrupt within ~3000ms, took %v", duration)
	}
}

func TestEngine_EnforcesCallBudget(t *testing.T) {
	engine, _ := setupTestEngine()
	ctx := context.Background()
	scope := testScope()

	code := `
		for (var i = 0; i < 55; i++) {
			await arda.crm.getCustomer({ customerId: "C-" + i });
		}
	`

	_, err := engine.Execute(ctx, scope, code)
	if err == nil {
		t.Fatal("expected budget exceeded error, got nil")
	}
	if !strings.Contains(err.Error(), "budget_exceeded") {
		t.Errorf("expected budget_exceeded error, got %v", err)
	}
}

func TestEngine_MutationYieldsApprovalNeeded(t *testing.T) {
	engine, _ := setupTestEngine()
	ctx := context.Background()
	scope := testScope()

	code := `
		const customer = await arda.crm.getCustomer({ customerId: "123" });
		const exportRes = await arda.crm.exportCustomer({ customerId: customer.id, format: "csv" });
		return exportRes;
	`

	result, err := engine.Execute(ctx, scope, code)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}

	if !result.ApprovalNeeded {
		t.Errorf("expected result.ApprovalNeeded to be true, got false")
	}
	if result.ProposalTool != "crm.exportCustomer" {
		t.Errorf("expected ProposalTool 'crm.exportCustomer', got '%s'", result.ProposalTool)
	}
}

func TestEngine_PermissionDenied(t *testing.T) {
	engine, _ := setupTestEngine()
	ctx := context.Background()
	// Scope without crm.customer.read permission
	scope := tools.Context{
		TenantID:    "tenant-test",
		ActorUserID: "user-test",
		Permissions: map[string]struct{}{},
	}

	code := `
		const res = await arda.crm.getCustomer({ customerId: "123" });
		return res;
	`

	_, err := engine.Execute(ctx, scope, code)
	if err == nil {
		t.Fatal("expected permission denied error, got nil")
	}
	if !strings.Contains(err.Error(), "permission_denied") {
		t.Errorf("expected permission_denied error, got: %v", err)
	}
}
