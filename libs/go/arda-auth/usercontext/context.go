package usercontext

import "context"

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
