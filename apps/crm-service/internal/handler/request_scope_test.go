package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestScopeRejectsMissingTenantOrOrganizations(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
	}{
		{name: "missing tenant", headers: map[string]string{"X-User-Org-Ids": "org-1"}},
		{name: "missing organizations", headers: map[string]string{"X-Tenant-Id": "tenant-1"}},
		{name: "active organization outside membership", headers: map[string]string{
			"X-Tenant-Id": "tenant-1", "X-User-Org-Ids": "org-1", "X-Org-Id": "org-2",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/crm/customers", nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			if err := ScopeFromRequest(req).Validate(); err == nil {
				t.Fatal("expected invalid request scope")
			}
		})
	}
}

func TestRequestScopeResolvesOnlyVerifiedOrganization(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/crm/customers", nil)
	req.Header.Set("X-Tenant-Id", "tenant-1")
	req.Header.Set("X-User-Org-Ids", "org-1, org-2")
	req.Header.Set("X-Org-Id", "org-2")

	scope := ScopeFromRequest(req)
	if err := scope.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := scope.ResolveOrgID(); got != "org-2" {
		t.Fatalf("active org = %q, want org-2", got)
	}
	if scope.AllowsOrg("org-3") {
		t.Fatal("organization outside verified membership was allowed")
	}
}
