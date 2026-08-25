package middleware

import (
	"context"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type contextKey string

const (
	// TenantIDKey is the context key for the tenant ID.
	TenantIDKey contextKey = "tenant_id"

	// TenantIDHeader is the HTTP header that carries the tenant ID.
	TenantIDHeader = "X-Tenant-Id"
)

// TenantContext returns the tenant ID from the request context.
func TenantContext(ctx context.Context) string {
	if v, ok := ctx.Value(TenantIDKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// WithTenant injects the tenant ID asserted by auth-gateway into the context.
// A missing or unverified tenant is rejected; this middleware never invents a
// tenant value because that would turn an absent scope into shared data access.
func WithTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Checked") != "true" {
			ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "verified tenant scope is required"))
			return
		}
		tenantID := strings.TrimSpace(r.Header.Get(TenantIDHeader))
		if tenantID == "" {
			ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "verified tenant scope is required"))
			return
		}

		ctx := context.WithValue(r.Context(), TenantIDKey, tenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
