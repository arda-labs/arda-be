package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/svcclient"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// testSecret satisfies identity.Issue's minimum length (32 chars).
const testSecret = "01234567890123456789012345678901"

func testIAMClient(baseURL string) *svcclient.IAMClient {
	return svcclient.NewIAMClient(baseURL, "ai-service", testSecret, nil)
}

func testCRMClient(baseURL string) *svcclient.CRMClient {
	return svcclient.NewCRMClient(baseURL, "ai-service", testSecret, nil)
}

func testFinanceClient(baseURL string) *svcclient.FinanceClient {
	return svcclient.NewFinanceClient(baseURL, "ai-service", testSecret, nil)
}

func iamScope() tools.Context {
	return tools.Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
		OrgIDs:      []string{"org-a", "org-b"},
		ActiveOrgID: "org-a",
		Username:    "nguyen.van.a",
		Email:       "nguyen.van.a@arda.io.vn",
		Roles:       []string{"crm_agent", "finance_viewer"},
		GlobalRoles: []string{"SUPER_ADMIN"},
		GlobalAdmin: true,
		Permissions: map[string]struct{}{
			"ai.assistant.use":  {},
			"crm.customer.read": {},
			"finance.read":      {},
		},
	}
}

func TestIAMMe_ReturnsIdentity(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterIAMCatalog(reg, testIAMClient("http://iam.local"))

	fn, entry, ok := reg.Resolve("iam.me")
	if !ok {
		t.Fatal("iam.me not registered")
	}
	if entry.Kind != "read" {
		t.Errorf("expected read kind, got %s", entry.Kind)
	}
	if err := entry.CheckPermissions(iamScope()); err != nil {
		t.Errorf("unexpected permission error: %v", err)
	}

	result, err := fn(context.Background(), iamScope(), nil)
	if err != nil {
		t.Fatalf("iam.me failed: %v", err)
	}
	me, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}

	user, _ := me["user"].(map[string]any)
	if user["id"] != "user-1" {
		t.Errorf("expected user.id user-1, got %v", user["id"])
	}
	if user["username"] != "nguyen.van.a" {
		t.Errorf("expected username nguyen.van.a, got %v", user["username"])
	}
	if me["tenant"].(map[string]any)["id"] != "tenant-1" {
		t.Errorf("expected tenant id tenant-1, got %v", me["tenant"])
	}
	if me["isGlobalAdmin"] != true {
		t.Errorf("expected isGlobalAdmin true, got %v", me["isGlobalAdmin"])
	}
	if len(me["permissions"].([]string)) != 3 {
		t.Errorf("expected 3 permissions, got %v", me["permissions"])
	}
}

func TestIAMMe_RequiresAssistantUse(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterIAMCatalog(reg, testIAMClient("http://iam.local"))

	_, entry, _ := reg.Resolve("iam.me")
	scope := iamScope()
	delete(scope.Permissions, "ai.assistant.use")
	if err := entry.CheckPermissions(scope); err == nil {
		t.Error("expected permission error without ai.assistant.use")
	}

	// superadmin bypass
	scope.Permissions = map[string]struct{}{"superadmin": {}}
	if err := entry.CheckPermissions(scope); err != nil {
		t.Errorf("superadmin should bypass, got %v", err)
	}
}

func TestIAMListUsers_RequiresIamUserRead(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterIAMCatalog(reg, testIAMClient("http://iam.local"))

	_, entry, ok := reg.Resolve("iam.listUsers")
	if !ok {
		t.Fatal("iam.listUsers not registered")
	}
	// Without iam.user.read → forbidden.
	scope := iamScope()
	if err := entry.CheckPermissions(scope); err == nil {
		t.Error("expected permission error without iam.user.read")
	}
	// With iam.user.read → allowed.
	scope.Permissions["iam.user.read"] = struct{}{}
	if err := entry.CheckPermissions(scope); err != nil {
		t.Errorf("expected allowed with iam.user.read, got %v", err)
	}
	// Not registered when IAM client is nil.
	regNoIAM := NewDispatcherRegistry()
	RegisterIAMCatalog(regNoIAM, nil)
	if _, _, ok := regNoIAM.Resolve("iam.listUsers"); ok {
		t.Error("iam.listUsers must not register without an IAM client")
	}
}

func TestIAMListUsers_DispatchesAndRedacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/ai/users" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant-1" {
			t.Errorf("expected tenant_id query tenant-1, got %s", got)
		}
		if r.Header.Get("X-User-Id") != "user-1" {
			t.Errorf("expected delegated X-User-Id user-1, got %s", r.Header.Get("X-User-Id"))
		}
		if r.Header.Get("X-Auth-Checked") != "true" {
			t.Error("expected X-Auth-Checked=true")
		}
		if r.Header.Get("X-Service-Auth") == "" {
			t.Error("expected signed x-service-auth caller assertion")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"result": {
				"items": [
					{"id":"u1","username":"a","email":"a@x.vn","name":"User A","status":"ACTIVE","roles":["crm_agent"],"address":"secret-address"},
					{"id":"u2","username":"b","email":"b@x.vn","name":"User B","status":"SUSPENDED","roles":[],"mobile":"0900"}
				],
				"page":1,"total":2
			},
			"success": true
		}`))
	}))
	defer server.Close()

	reg := NewDispatcherRegistry()
	RegisterIAMCatalog(reg, testIAMClient(server.URL))

	fn, entry, ok := reg.Resolve("iam.listUsers")
	if !ok {
		t.Fatal("iam.listUsers not registered")
	}
	scope := iamScope()
	scope.Permissions["iam.user.read"] = struct{}{}
	if err := entry.CheckPermissions(scope); err != nil {
		t.Fatalf("permission check failed: %v", err)
	}

	result, err := fn(context.Background(), scope, map[string]any{"limit": float64(20)})
	if err != nil {
		t.Fatalf("listUsers failed: %v", err)
	}
	page, ok := result.(svcclient.UserListPage)
	if !ok {
		t.Fatalf("unexpected result type %T", result)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(page.Items))
	}
	// Redaction: typed decode drops sensitive fields (address, mobile) that
	// are not part of the UserSummary shape.
	if page.Items[0].Roles[0] != "crm_agent" {
		t.Errorf("expected roles preserved, got %v", page.Items[0].Roles)
	}
}

func TestIAMListCapabilities_FiltersByPermissionAndSearch(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterIAMCatalog(reg, testIAMClient("http://iam.local"))
	// Register extra entries across domains to exercise filtering.
	RegisterCRMCatalog(reg, testCRMClient("http://crm.local"))
	RegisterKnowledgeCatalog(reg, nil) // nil searcher → not registered
	RegisterFinanceCatalog(reg, testFinanceClient("http://finance.local"))

	scope := iamScope() // crm.customer.read + finance.read

	result, err := listCapabilities(reg, scope, map[string]any{})
	if err != nil {
		t.Fatalf("listCapabilities failed: %v", err)
	}
	page := result.(map[string]any)
	items := page["items"].([]map[string]any)

	// Scope has crm.customer.read and finance.read but NOT crm.customer.export
	// nor ai.knowledge.read → those must be filtered out.
	seen := map[string]bool{}
	for _, item := range items {
		seen[item["sdkPath"].(string)] = true
	}
	if !seen["arda.iam.me"] {
		t.Error("expected arda.iam.me in capabilities")
	}
	if !seen["arda.crm.getCustomer"] {
		t.Error("expected arda.crm.getCustomer in capabilities")
	}
	if !seen["arda.finance.getAccount"] {
		t.Error("expected arda.finance.getAccount in capabilities")
	}
	if seen["arda.crm.exportCustomer"] {
		t.Error("arda.crm.exportCustomer must be filtered: missing crm.customer.export")
	}
	if seen["arda.knowledge.search"] {
		t.Error("arda.knowledge.search must be filtered: nil searcher")
	}
}

func TestIAMListCapabilities_SearchAndPagination(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterIAMCatalog(reg, testIAMClient("http://iam.local"))
	RegisterCRMCatalog(reg, testCRMClient("http://crm.local"))
	RegisterFinanceCatalog(reg, testFinanceClient("http://finance.local"))

	scope := iamScope()

	// Search narrows results.
	result, err := listCapabilities(reg, scope, map[string]any{"search": "customer"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	page := result.(map[string]any)
	for _, item := range page["items"].([]map[string]any) {
		path := item["sdkPath"].(string)
		if path != "arda.crm.getCustomer" && path != "arda.crm.exportCustomer" {
			t.Errorf("search 'customer' returned unrelated %s", path)
		}
	}

	// Domain filter.
	result, err = listCapabilities(reg, scope, map[string]any{"domain": "finance"})
	if err != nil {
		t.Fatalf("domain filter failed: %v", err)
	}
	page = result.(map[string]any)
	for _, item := range page["items"].([]map[string]any) {
		if item["domain"] != "finance" {
			t.Errorf("domain filter leaked %v", item["domain"])
		}
	}

	// Pagination: limit 1 → hasMore true, nextCursor advances.
	result, err = listCapabilities(reg, scope, map[string]any{"limit": float64(1)})
	if err != nil {
		t.Fatalf("pagination failed: %v", err)
	}
	page = result.(map[string]any)
	if len(page["items"].([]map[string]any)) != 1 {
		t.Fatalf("expected 1 item with limit=1, got %d", len(page["items"].([]map[string]any)))
	}
	if page["hasMore"] != true {
		t.Error("expected hasMore=true with limit=1")
	}
}
