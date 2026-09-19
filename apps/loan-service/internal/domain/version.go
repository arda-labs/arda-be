package domain

import "context"

type dataVersionKey struct{}

// WithDataVersion stamps the row version the checker saw onto the context. The
// guarded repository transitions read it and refuse a mismatch, turning a stale
// approval into a conflict instead of silently applying it.
func WithDataVersion(ctx context.Context, version int64) context.Context {
	return context.WithValue(ctx, dataVersionKey{}, version)
}

// DataVersionFromContext returns the expected row version (0 = unguarded).
func DataVersionFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(dataVersionKey{}).(int64)
	return v
}
