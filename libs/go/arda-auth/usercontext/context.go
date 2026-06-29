package usercontext

import (
	"context"
	"net/http"
	"strings"
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
		UserID:      strings.TrimSpace(h.Get("X-User-Id")),
		Subject:     strings.TrimSpace(h.Get("X-User-Subject")),
		Username:    strings.TrimSpace(h.Get("X-Username")),
		TenantID:    strings.TrimSpace(h.Get("X-Tenant-Id")),
		Roles:       splitHeader(h.Get("X-Roles")),
		Permissions: splitHeader(h.Get("X-Permissions")),
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
