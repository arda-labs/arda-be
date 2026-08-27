package usercontext

import (
	"context"
	"net/http"
	"strings"
)

// Canonical gateway-forwarded identity headers. These are the single source
// of truth shared with auth-gateway and mirrored by libs/go/arda-grpc/metadata
// for the gRPC hop; never inline a raw header name at a call site.
const (
	HeaderUserID      = "X-User-Id"
	HeaderUserSubject = "X-User-Subject"
	HeaderUsername    = "X-Username"
	HeaderTenantID    = "X-Tenant-Id"
	HeaderOrgID       = "X-Org-Id"
	HeaderRoles       = "X-Roles"
	HeaderPermissions = "X-Permissions"
	HeaderAuthChecked = "X-Auth-Checked"
	HeaderGlobalAdmin = "X-Global-Admin"
)

// UserContext is the standard auth context shared across services.
type UserContext struct {
	UserID      string
	Subject     string
	Username    string
	TenantID    string
	Roles       []string
	Permissions []string
}

type ctxKey struct{}

// WithContext injects a UserContext into the context.
func WithContext(ctx context.Context, uc *UserContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, uc)
}

// FromContext extracts a UserContext from the context.
func FromContext(ctx context.Context) (*UserContext, bool) {
	uc, ok := ctx.Value(ctxKey{}).(*UserContext)
	return uc, ok
}

// FromHeaders builds the standard user context forwarded by auth-gateway.
func FromHeaders(h http.Header) *UserContext {
	return &UserContext{
		UserID:      strings.TrimSpace(h.Get(HeaderUserID)),
		Subject:     strings.TrimSpace(h.Get(HeaderUserSubject)),
		Username:    strings.TrimSpace(h.Get(HeaderUsername)),
		TenantID:    strings.TrimSpace(h.Get(HeaderTenantID)),
		Roles:       splitHeader(h.Get(HeaderRoles)),
		Permissions: splitHeader(h.Get(HeaderPermissions)),
	}
}

func splitHeader(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
