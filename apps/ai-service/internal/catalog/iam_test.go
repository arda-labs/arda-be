package catalog

import (
	"context"
	"encoding/json"
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

func testHRMClient(baseURL string) *svcclient.HRMClient {
	return svcclient.NewHRMClient(baseURL, "ai-service", testSecret, nil)
}

func genClients(iamURL, crmURL, financeURL string) ClientSet {
	return ClientSet{
		IAM:     testIAMClient(iamURL),
		CRM:     testCRMClient(crmURL),
		Finance: testFinanceClient(financeURL),
	}
}

func genClientsWithHRM(hrmURL string) ClientSet {
	set := genClients("http://iam.local", "http://crm.local", "http://finance.local")
	set.HRM = testHRMClient(hrmURL)
	return set
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

func registerFullCatalog(reg *DispatcherRegistry, set ClientSet) {
	RegisterBuiltinCatalog(reg, nil)
	RegisterGeneratedCatalog(reg, set)
}

func TestIAMMe_ReturnsIdentity(t *testing.T) {
	reg := NewDispatcherRegistry()
	RegisterIAMCatalog(reg)

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
	RegisterIAMCatalog(reg)

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

func TestGeneratedCatalog_RegistersAnnotatedEntries(t *testing.T) {
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClients("http://iam.local", "http://crm.local", "http://finance.local"))

	for _, method := range []string{"iam.listUsers", "crm.getCustomer", "finance.getAccount"} {
		if _, entry, ok := reg.Resolve(method); !ok {
			t.Errorf("%s not registered from contract", method)
		} else if entry.Kind != "read" {
			t.Errorf("%s: expected read kind, got %s", method, entry.Kind)
		}
	}

	// Export stays manual and confirm-kind.
	_, entry, ok := reg.Resolve("crm.exportCustomer")
	if !ok {
		t.Fatal("crm.exportCustomer not registered")
	}
	if entry.Kind != "confirm" {
		t.Errorf("export: expected confirm kind, got %s", entry.Kind)
	}
	if entry.RequiredPermissions[0] != "crm.customer.manage" {
		t.Errorf("export: expected crm.customer.manage, got %v", entry.RequiredPermissions)
	}
}

func TestGeneratedCatalog_SkipsUnwiredServices(t *testing.T) {
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, ClientSet{})

	for _, method := range []string{"iam.listUsers", "crm.getCustomer", "finance.getAccount"} {
		if _, _, ok := reg.Resolve(method); ok {
			t.Errorf("%s must not register without a service client", method)
		}
	}
	// Self-service manual entries do not depend on service clients.
	if _, _, ok := reg.Resolve("iam.me"); !ok {
		t.Error("iam.me must register without any service client")
	}
}

func TestGeneratedCatalog_ListUsersPermissions(t *testing.T) {
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClients("http://iam.local", "http://crm.local", "http://finance.local"))

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
}

func TestGeneratedCatalog_ListUsersDispatchRedacts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/ai/users" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant-1" {
			t.Errorf("expected tenant_id query tenant-1, got %s", got)
		}
		if got := r.URL.Query().Get("size"); got != "20" {
			t.Errorf("expected size=20, got %s", got)
		}
		if got := r.URL.Query().Get("q"); got != "nguyen" {
			t.Errorf("expected q=nguyen from search arg, got %s", got)
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
	registerFullCatalog(reg, genClients(server.URL, "", ""))

	fn, _, ok := reg.Resolve("iam.listUsers")
	if !ok {
		t.Fatal("iam.listUsers not registered")
	}
	result, err := fn(context.Background(), iamScope(), map[string]any{"limit": float64(20), "search": "nguyen"})
	if err != nil {
		t.Fatalf("listUsers failed: %v", err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(page.Items) != 2 || page.Total != 2 {
		t.Fatalf("expected 2 items/total 2, got %d/%d", len(page.Items), page.Total)
	}
	// Redaction: response allowlist drops fields not declared in the contract
	// (address, mobile) even though the server returned them.
	for key := range page.Items[0] {
		if key == "address" || key == "mobile" {
			t.Errorf("sensitive field %s leaked through the response allowlist", key)
		}
	}
	if page.Items[0]["id"] != "u1" {
		t.Errorf("expected id u1, got %v", page.Items[0]["id"])
	}
}

func TestGeneratedCustomerDispatch_PathParamsAndRedaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/ai/customers/C-1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":{"id":"C-1","customerCode":"KH-001","name":"Cong ty A","identityNumbers":["0123"],"infoMap":{"x":1}},"success":true}`))
	}))
	defer server.Close()

	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClients("", server.URL, ""))

	fn, _, ok := reg.Resolve("crm.getCustomer")
	if !ok {
		t.Fatal("crm.getCustomer not registered")
	}
	result, err := fn(context.Background(), iamScope(), map[string]any{"customerId": "C-1"})
	if err != nil {
		t.Fatalf("getCustomer failed: %v", err)
	}
	raw, _ := json.Marshal(result)
	var customer map[string]any
	if err := json.Unmarshal(raw, &customer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if customer["customerCode"] != "KH-001" {
		t.Errorf("expected customerCode KH-001, got %v", customer["customerCode"])
	}
	if _, leaked := customer["identityNumbers"]; leaked {
		t.Error("identityNumbers leaked through the response allowlist")
	}
	if _, leaked := customer["infoMap"]; leaked {
		t.Error("infoMap leaked through the response allowlist")
	}
}

func TestGeneratedCatalog_ArgValidation(t *testing.T) {
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClients("http://iam.local", "http://crm.local", "http://finance.local"))

	cases := []struct {
		name   string
		path   string
		args   map[string]any
		expect string
	}{
		{"search too long", "iam.listUsers", map[string]any{"search": string(make([]byte, 200))}, "too long"},
		{"limit over max", "iam.listUsers", map[string]any{"limit": float64(100)}, "must be <="},
		{"limit not number", "iam.listUsers", map[string]any{"limit": "ten"}, "must be a number"},
		{"status not enum", "iam.listUsers", map[string]any{"status": "GHOST"}, "must be one of"},
		{"customerId required", "crm.getCustomer", map[string]any{}, "required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn, _, ok := reg.Resolve(tc.path)
			if !ok {
				t.Fatalf("%s not registered", tc.path)
			}
			_, err := fn(context.Background(), iamScope(), tc.args)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.expect)
			}
			if !contains(err.Error(), tc.expect) {
				t.Fatalf("expected error containing %q, got %v", tc.expect, err)
			}
		})
	}
}

func TestGeneratedCatalog_FinanceDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/ai/accounts/ACC-1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":{"account":{"id":"ACC-1","code":"111","name":"Cash","type":"ASSET","normalBalance":"DEBIT","currency":"VND","isActive":true},"balance":{"amount":1000,"currency":"VND"}},"success":true}`))
	}))
	defer server.Close()

	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClients("", "", server.URL))

	fn, _, ok := reg.Resolve("finance.getAccount")
	if !ok {
		t.Fatal("finance.getAccount not registered")
	}
	result, err := fn(context.Background(), iamScope(), map[string]any{"accountId": "ACC-1"})
	if err != nil {
		t.Fatalf("getAccount failed: %v", err)
	}
	raw, _ := json.Marshal(result)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	account, _ := payload["account"].(map[string]any)
	if account == nil || account["code"] != "111" {
		t.Errorf("expected account.code 111, got %v", payload)
	}
	// Untyped balance passes through the allowlist untouched.
	if _, ok := payload["balance"].(map[string]any); !ok {
		t.Errorf("expected balance object passthrough, got %v", payload["balance"])
	}
}

func TestGeneratedCatalog_HRMDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/ai/employees" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "nguyen" {
			t.Errorf("expected q=nguyen from search arg, got %s", got)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "tenant-1" {
			t.Errorf("expected tenant_id=tenant-1 from scope, got %s", got)
		}
		// The server returns internal linkage fields the allowlist must drop.
		_, _ = w.Write([]byte(`{"result":{"items":[
			{"id":"e1","employeeCode":"NV-001","fullName":"Nguyen Van A","status":"ACTIVE","tenant_id":"tenant-1","iamUserId":"iam-1"}
		],"page":1,"per_page":20,"total":1},"success":true}`))
	}))
	defer server.Close()

	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClientsWithHRM(server.URL))

	fn, entry, ok := reg.Resolve("hrm.listEmployees")
	if !ok {
		t.Fatal("hrm.listEmployees not registered")
	}
	if entry.RequiredPermissions[0] != "hrm.read" {
		t.Errorf("expected hrm.read, got %v", entry.RequiredPermissions)
	}
	result, err := fn(context.Background(), iamScope(), map[string]any{"search": "nguyen"})
	if err != nil {
		t.Fatalf("listEmployees failed: %v", err)
	}
	raw, _ := json.Marshal(result)
	var page struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Items) != 1 || page.Total != 1 {
		t.Fatalf("expected 1 item/total 1, got %d/%d", len(page.Items), page.Total)
	}
	for key := range page.Items[0] {
		if key == "tenant_id" || key == "iamUserId" {
			t.Errorf("internal field %s leaked through the response allowlist", key)
		}
	}
	if page.Items[0]["employeeCode"] != "NV-001" {
		t.Errorf("expected employeeCode NV-001, got %v", page.Items[0]["employeeCode"])
	}
	// Without hrm.read the entry is forbidden for the actor.
	if err := entry.CheckPermissions(iamScope()); err == nil {
		t.Error("expected permission error without hrm.read")
	}
}

func TestIAMListCapabilities_FiltersByPermissionAndSearch(t *testing.T) {
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClients("http://iam.local", "http://crm.local", "http://finance.local"))

	scope := iamScope() // crm.customer.read + finance.read

	result, err := listCapabilities(reg, scope, map[string]any{})
	if err != nil {
		t.Fatalf("listCapabilities failed: %v", err)
	}
	page := result.(map[string]any)
	items := page["items"].([]map[string]any)

	// Scope has crm.customer.read and finance.read but NOT crm.customer.manage
	// nor iam.user.read nor ai.knowledge.read → those must be filtered out.
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
		t.Error("arda.crm.exportCustomer must be filtered: missing crm.customer.manage")
	}
	if seen["arda.iam.listUsers"] {
		t.Error("arda.iam.listUsers must be filtered: missing iam.user.read")
	}
	if seen["arda.knowledge.search"] {
		t.Error("arda.knowledge.search must be filtered: nil searcher")
	}
}

func TestIAMListCapabilities_SearchAndPagination(t *testing.T) {
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, genClients("http://iam.local", "http://crm.local", "http://finance.local"))

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

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
