package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func TestBuildIdentityContext_MinimalIdentityOnly(t *testing.T) {
	scope := tools.Context{
		TenantID:    "tenant-1",
		ActorUserID: "user-1",
		ActiveOrgID: "org-a",
		Username:    "nguyen.van.a",
		// Permissions deliberately NOT injected into the prompt.
		Permissions: map[string]struct{}{
			"superadmin":          {},
			"crm.customer.read":   {},
			"crm.customer.export": {},
		},
	}
	prompt := buildIdentityContext(scope)

	for _, want := range []string{"user-1", "tenant-1", "org-a", "nguyen.van.a", "Authorization is enforced at execution time"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// The permission catalog must NOT be dumped into the prompt.
	for _, forbidden := range []string{"crm.customer.read", "crm.customer.export", "superadmin"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("prompt leaked permission %q — must stay out of the prompt", forbidden)
		}
	}
}

func TestBuildIdentityContext_EmptyScope(t *testing.T) {
	prompt := buildIdentityContext(tools.Context{})
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("expected non-empty authorization guidance even with empty scope")
	}
	if strings.Contains(prompt, "- user_id:") {
		t.Error("should not emit empty user_id line")
	}
}

func TestScopeFromRequest_ParsesIdentityHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/ai/agent", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-User-Org-Ids", "org-a,org-b")
	req.Header.Set("X-Org-Id", "org-a")
	req.Header.Set("X-Username", "nguyen.van.a")
	req.Header.Set("X-User-Email", "nguyen.van.a@arda.io.vn")
	req.Header.Set("X-Roles", "crm_agent,finance_viewer")
	req.Header.Set("X-Global-Roles", "SUPER_ADMIN")
	req.Header.Set("X-Global-Admin", "true")
	req.Header.Set("X-Permissions", "ai.assistant.use,crm.customer.read")
	req.Header.Set("X-Auth-Version", "7")

	scope := scopeFromRequest(req)

	if scope.TenantID != "tenant-1" || scope.ActorUserID != "user-1" {
		t.Errorf("unexpected tenant/user: %s / %s", scope.TenantID, scope.ActorUserID)
	}
	if len(scope.OrgIDs) != 2 || scope.ActiveOrgID != "org-a" {
		t.Errorf("unexpected orgs: %v active=%s", scope.OrgIDs, scope.ActiveOrgID)
	}
	if scope.Username != "nguyen.van.a" || scope.Email != "nguyen.van.a@arda.io.vn" {
		t.Errorf("unexpected identity: %s / %s", scope.Username, scope.Email)
	}
	if len(scope.Roles) != 2 || scope.Roles[0] != "crm_agent" {
		t.Errorf("unexpected roles: %v", scope.Roles)
	}
	if len(scope.GlobalRoles) != 1 || !scope.GlobalAdmin {
		t.Errorf("unexpected global: %v admin=%v", scope.GlobalRoles, scope.GlobalAdmin)
	}
	if scope.AuthVersion != "7" {
		t.Errorf("unexpected auth version: %s", scope.AuthVersion)
	}
	if _, ok := scope.Permissions["crm.customer.read"]; !ok {
		t.Error("expected crm.customer.read in permissions")
	}
}

func TestScopeFromRequest_GlobalAdminFalseWhenAbsent(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/ai/agent", nil)
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "tenant-1")

	scope := scopeFromRequest(req)
	if scope.GlobalAdmin {
		t.Error("X-Global-Admin absent should yield GlobalAdmin=false")
	}
	if scope.Username != "" || scope.Email != "" {
		t.Error("absent identity headers should stay empty")
	}
}
