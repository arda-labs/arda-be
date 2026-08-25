package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithTenantRequiresVerifiedNonEmptyScope(t *testing.T) {
	tests := []struct {
		name       string
		authCheck  string
		tenant     string
		wantStatus int
	}{
		{name: "missing auth assertion", tenant: "tenant-a", wantStatus: http.StatusForbidden},
		{name: "missing tenant", authCheck: "true", wantStatus: http.StatusBadRequest},
		{name: "verified tenant", authCheck: "true", tenant: " tenant-a ", wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := WithTenant(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := TenantContext(r.Context()); got != "tenant-a" {
					t.Errorf("TenantContext = %q, want tenant-a", got)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("X-Auth-Checked", tt.authCheck)
			req.Header.Set(TenantIDHeader, tt.tenant)
			res := httptest.NewRecorder()

			handler.ServeHTTP(res, req)
			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", res.Code, tt.wantStatus)
			}
		})
	}
}
