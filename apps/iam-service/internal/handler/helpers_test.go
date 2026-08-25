package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateAdminTargetTenantRequiresVerifiedActor(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		tenant     string
		wantStatus int
		wantOK     bool
	}{
		{
			name:       "missing target tenant",
			headers:    verifiedAdminHeaders("tenant-a"),
			tenant:     "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing verified actor",
			headers:    map[string]string{"X-Tenant-Id": "tenant-a"},
			tenant:     "tenant-a",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "same tenant",
			headers:    verifiedAdminHeaders("tenant-a"),
			tenant:     "tenant-a",
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "cross tenant without global capability",
			headers:    verifiedAdminHeaders("tenant-a"),
			tenant:     "tenant-b",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "cross tenant with super admin role",
			headers:    withHeader(verifiedAdminHeaders("tenant-a"), "X-Roles", "SUPER_ADMIN"),
			tenant:     "tenant-b",
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "cross tenant with gateway global capability",
			headers:    withHeader(verifiedAdminHeaders("tenant-a"), "X-Global-Admin", "true"),
			tenant:     "tenant-b",
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			rec := httptest.NewRecorder()

			got, ok := validateAdminTargetTenant(rec, req, tt.tenant)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (tenant=%q)", ok, tt.wantOK, got)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantOK && got != tt.tenant {
				t.Fatalf("tenant = %q, want %q", got, tt.tenant)
			}
			if !tt.wantOK {
				var problem map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
					t.Fatalf("decode problem: %v", err)
				}
				if problem["code"] == nil || problem["message"] == nil {
					t.Fatalf("problem lacks stable code/message: %#v", problem)
				}
			}
		})
	}
}

func verifiedAdminHeaders(tenant string) map[string]string {
	return map[string]string{
		"X-Auth-Checked": "true",
		"X-User-Id":      "actor-1",
		"X-Tenant-Id":    tenant,
	}
}

func withHeader(headers map[string]string, key, value string) map[string]string {
	copyHeaders := make(map[string]string, len(headers)+1)
	for name, item := range headers {
		copyHeaders[name] = item
	}
	copyHeaders[key] = value
	return copyHeaders
}
