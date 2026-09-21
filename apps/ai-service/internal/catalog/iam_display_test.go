package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func identityResult(t *testing.T, reg *DispatcherRegistry, scope tools.Context) map[string]any {
	t.Helper()
	fn, _, _ := reg.Resolve("iam.me")
	result, err := fn(context.Background(), scope, map[string]any{"tenantId": "other", "userId": "other"})
	if err != nil {
		t.Fatal(err)
	}
	return result.(map[string]any)
}

func TestIAMMeDisplaySignedScopedAndRedacted(t *testing.T) {
	pages := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Tenant-Id") != "tenant-1" || r.Header.Get("X-User-Id") != "user-1" || r.Header.Get("X-Service-Auth") == "" {
			t.Error("missing signed delegated identity")
		}
		switch r.URL.Path {
		case "/internal/ai/me/display":
			fmt.Fprint(w, `{"result":{"user":{"id":"user-1","name":"Nguyen Van A","phone":"private-phone"},"tenant":{"id":"tenant-1","code":"ARDA","name":"Arda Labs"},"otherMemberships":["private-tenant"]}}`)
		case "/internal/ai/organizations":
			if r.URL.Query().Get("tenant_id") != "tenant-1" {
				t.Error("directory not tenant scoped")
			}
			pages++
			items := []map[string]any{{"id": "org-b", "name": "Branch B", "code": "B", "address": "private-address"}}
			if r.URL.Query().Get("page") == "1" {
				items = []map[string]any{{"id": "org-a", "name": "Head office", "code": "HQ"}}
				for i := 1; i < 20; i++ {
					items = append(items, map[string]any{"id": fmt.Sprint(i), "name": "unrelated-org"})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"items": items, "total": 21}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, ClientSet{"iam-service": testClient("iam-service", server.URL), "platform-service": testClient("platform-service", server.URL)})
	scope := iamScope()
	scope.Permissions["platform.read"] = struct{}{}
	me := identityResult(t, reg, scope)
	if me["tenant"].(map[string]any)["name"] != "Arda Labs" || me["user"].(map[string]any)["name"] != "Nguyen Van A" {
		t.Fatalf("missing names: %#v", me)
	}
	details := me["organizationDetails"].([]map[string]any)
	if pages != 2 || len(details) != 2 || details[0]["code"] != "HQ" || details[1]["name"] != "Branch B" {
		t.Fatalf("bad org resolution: pages=%d %#v", pages, details)
	}
	if me["displayResolution"].(map[string]any)["organizations"] != "resolved" {
		t.Fatal("expected resolved")
	}
	encoded, _ := json.Marshal(me)
	for _, forbidden := range []string{"private-phone", "private-tenant", "private-address", "unrelated-org"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("leaked %s", forbidden)
		}
	}
	if len(me["organizations"].([]string)) != 2 || len(me["permissions"].([]string)) != 4 {
		t.Fatal("changed identity contract")
	}
}

func TestIAMMeDisplayHonorsPermissionsAndGovernance(t *testing.T) {
	for _, scenario := range []string{"no-permission", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			reg := NewDispatcherRegistry()
			RegisterIAMCatalog(reg)
			reg.Register(CatalogEntry{MethodName: "platform.listOrganizations", Kind: "read", Enabled: true, RequiredPermissions: []string{"platform.read"}}, func(context.Context, tools.Context, map[string]any) (any, error) {
				t.Fatal("forbidden nested read executed")
				return nil, nil
			})
			scope := iamScope()
			if scenario == "disabled" {
				scope.Permissions["platform.read"] = struct{}{}
				reg.SetEnabledPredicate(func(e CatalogEntry) bool { return e.MethodName != "platform.listOrganizations" })
			}
			me := identityResult(t, reg, scope)
			if me["displayResolution"].(map[string]any)["organizations"] != "unavailable" {
				t.Fatal("expected unavailable labels")
			}
		})
	}
}

func TestIAMMeDisplayRejectsMismatchedTenant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"result":{"user":{"id":"user-1","name":"wrong-user-label"},"tenant":{"id":"other","name":"wrong-tenant-label"}}}`)
	}))
	defer server.Close()
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, ClientSet{"iam-service": testClient("iam-service", server.URL)})
	me := identityResult(t, reg, iamScope())
	if me["tenant"].(map[string]any)["name"] != nil || me["user"].(map[string]any)["name"] != nil {
		t.Fatal("accepted mismatched response")
	}
}

func TestIAMMeDisplayBoundsPaginationAndPreservesPartialIdentity(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/ai/me/display" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		calls++
		items := []map[string]any{{"id": "org-a", "name": "Head office"}}
		for i := 1; i < 20; i++ {
			items = append(items, map[string]any{"id": fmt.Sprint(i)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"items": items, "total": 10000}})
	}))
	defer server.Close()
	reg := NewDispatcherRegistry()
	registerFullCatalog(reg, ClientSet{"iam-service": testClient("iam-service", server.URL), "platform-service": testClient("platform-service", server.URL)})
	scope := iamScope()
	scope.Permissions["platform.read"] = struct{}{}
	me := identityResult(t, reg, scope)
	resolution := me["displayResolution"].(map[string]any)
	if calls != 3 || resolution["tenant"] != "unavailable" || resolution["organizations"] != "partial" {
		t.Fatalf("calls=%d resolution=%v", calls, resolution)
	}
	if me["user"].(map[string]any)["username"] != scope.Username {
		t.Fatal("lost identity after lookup failure")
	}
}
