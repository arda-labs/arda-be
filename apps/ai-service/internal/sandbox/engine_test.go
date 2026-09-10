package sandbox

import (
	"context"
	"errors"
	"fmt"
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

func TestEngine_EnforcesPerMethodCallLimit(t *testing.T) {
	engine, _ := setupTestEngine()
	code := fmt.Sprintf(`
		for (var i = 0; i < %d; i++) {
			await arda.crm.getCustomer({ customerId: "C-" + i });
		}
	`, MaxMethodCallsPerRun+1)

	_, err := engine.Execute(context.Background(), testScope(), code)
	if err == nil {
		t.Fatal("expected per-method budget error, got nil")
	}
	if !strings.Contains(err.Error(), "budget_exceeded") {
		t.Fatalf("expected budget_exceeded, got %v", err)
	}
}

func TestEngine_PerTenantConcurrencyGate(t *testing.T) {
	engine, _ := setupTestEngine()
	sem := engine.tenantSemaphore("tenant-test")
	for i := 0; i < MaxConcurrentSandboxesPerTenant; i++ {
		sem <- struct{}{}
	}

	_, err := engine.Execute(context.Background(), testScope(), `return 1;`)
	if !errors.Is(err, ErrSandboxBusy) {
		t.Fatalf("expected ErrSandboxBusy when the tenant is at capacity, got %v", err)
	}

	// A different tenant still gets its own slots.
	other := tools.Context{
		TenantID:    "tenant-other",
		ActorUserID: "user-test",
		Permissions: map[string]struct{}{"crm.customer.read": {}},
	}
	if _, err := engine.Execute(context.Background(), other, `return 1;`); err != nil {
		t.Fatalf("other tenant must not be blocked by a saturated tenant: %v", err)
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

// Prototype-chain escapes survive a naive token list because they never spell
// out eval/Function. They must still be rejected statically.
func TestStaticValidator_BlocksPrototypeChainEscapes(t *testing.T) {
	escapes := []string{
		`return ({}).constructor.constructor("return 1")();`,
		`const c = ({}).constructor; return c.constructor("return process")();`,
		`return ({}).__proto__;`,
		`return Object["defineProperty"]({}, "x", {});`,
		`return ({})["constructor"];`,
		`return [].prototype;`,
		`return Object.getPrototypeOf({});`,
		`const p = Proxy; return p;`,
	}
	for _, code := range escapes {
		if err := ValidateScript(code); err == nil {
			t.Errorf("expected escape rejected: %s", code)
		}
	}
}

func TestStaticValidator_AllowsFunctionKeyword(t *testing.T) {
	safe := []string{
		`const f = function (x) { return x + 1; }; return f(1);`,
		`async function run() { return 1; } return run();`,
	}
	for _, code := range safe {
		if err := ValidateScript(code); err != nil {
			t.Errorf("safe script rejected: %s (%v)", code, err)
		}
	}
}

func TestEngine_RejectsOversizedScript(t *testing.T) {
	engine, _ := setupTestEngine()
	code := "return 1;" + strings.Repeat(" ", 17*1024)
	if _, err := engine.Execute(context.Background(), testScope(), code); err == nil || !strings.Contains(err.Error(), "too_large") {
		t.Fatalf("expected script-too-large error, got %v", err)
	}
}

func TestEngine_RejectsOversizedOutput(t *testing.T) {
	engine, _ := setupTestEngine()
	result, err := engine.Execute(context.Background(), testScope(), `return "x".repeat(70000);`)
	if err == nil {
		t.Fatal("expected output-too-large error, got nil")
	}
	if !strings.Contains(err.Error(), "ai.sandbox_output_too_large") {
		t.Fatalf("expected ai.sandbox_output_too_large, got %v", err)
	}
	if result.Output != nil {
		t.Fatalf("oversized output must be dropped, got %v", result.Output)
	}
}
