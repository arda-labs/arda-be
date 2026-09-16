package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveAdminReadTenant(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		query      string
		wantTenant string
		wantOK     bool
		wantStatus int
	}{
		{
			name:       "tenant administrator without filter reads own tenant",
			headers:    verifiedAdminHeaders("tenant-a"),
			wantTenant: "tenant-a",
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "tenant administrator with own tenant filter",
			headers:    verifiedAdminHeaders("tenant-a"),
			query:      "?tenant_id=tenant-a",
			wantTenant: "tenant-a",
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "tenant administrator cannot read another tenant",
			headers:    verifiedAdminHeaders("tenant-a"),
			query:      "?tenant_id=tenant-b",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "tenant administrator cannot read another tenant through camel case alias",
			headers:    verifiedAdminHeaders("tenant-a"),
			query:      "?tenantId=tenant-b",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "global administrator without filter reads every tenant",
			headers:    withHeader(verifiedAdminHeaders("tenant-a"), "X-Global-Admin", "true"),
			wantTenant: "",
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "global administrator can target another tenant",
			headers:    withHeader(verifiedAdminHeaders("tenant-a"), "X-Global-Admin", "true"),
			query:      "?tenant_id=tenant-b",
			wantTenant: "tenant-b",
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "global super admin role reads every tenant",
			headers:    withHeader(verifiedAdminHeaders("tenant-a"), "X-Global-Roles", "SUPER_ADMIN"),
			wantTenant: "",
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "tenant scoped super admin role is not global",
			headers:    withHeader(verifiedAdminHeaders("tenant-a"), "X-Roles", "SUPER_ADMIN"),
			query:      "?tenant_id=tenant-b",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing verified actor",
			headers:    map[string]string{"X-Tenant-Id": "tenant-a"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing verified actor tenant",
			headers:    map[string]string{"X-Auth-Checked": "true", "X-User-Id": "actor-1"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "global administrator without actor tenant reads every tenant",
			headers:    map[string]string{"X-Auth-Checked": "true", "X-User-Id": "actor-1", "X-Global-Admin": "true"},
			wantTenant: "",
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/admin/audit"+tt.query, nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			rec := httptest.NewRecorder()

			got, ok := resolveAdminReadTenant(rec, req)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (tenant=%q)", ok, tt.wantOK, got)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantOK && got != tt.wantTenant {
				t.Fatalf("tenant = %q, want %q", got, tt.wantTenant)
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

func TestAuditEndpointsRejectCrossTenantRead(t *testing.T) {
	h := NewAuditHandler(nil)
	endpoints := []struct {
		name   string
		target string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "query", target: "/api/admin/audit?tenant_id=tenant-b", call: h.Query},
		{name: "export", target: "/api/admin/audit/export?tenant_id=tenant-b", call: h.ExportAudit},
		{name: "stats", target: "/api/admin/audit/stats?tenant_id=tenant-b", call: h.Stats},
		{name: "verify", target: "/api/admin/audit/verify?tenant_id=tenant-b", call: h.Verify},
	}

	for _, tt := range endpoints {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			for key, value := range verifiedAdminHeaders("tenant-a") {
				req.Header.Set(key, value)
			}
			rec := httptest.NewRecorder()

			tt.call(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			var problem map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem["message"] != "actor cannot read another tenant" {
				t.Fatalf("message = %v, want cross-tenant rejection", problem["message"])
			}
		})
	}
}

func TestAuditEndpointsRejectMissingVerifiedActor(t *testing.T) {
	h := NewAuditHandler(nil)
	endpoints := []struct {
		name   string
		target string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "query", target: "/api/admin/audit", call: h.Query},
		{name: "export", target: "/api/admin/audit/export", call: h.ExportAudit},
		{name: "stats", target: "/api/admin/audit/stats", call: h.Stats},
		{name: "verify", target: "/api/admin/audit/verify", call: h.Verify},
	}

	for _, tt := range endpoints {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.call(rec, httptest.NewRequest(http.MethodGet, tt.target, nil))

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
		})
	}
}
