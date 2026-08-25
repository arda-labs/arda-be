package interceptors

import (
	"context"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type serviceClaimsContextKey struct{}

// UnaryClientServiceAuth signs every internal call with the workload identity
// of sourceService and the explicit destination audience.
func UnaryClientServiceAuth(secret, sourceService, audience string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		token, err := identity.Issue(secret, sourceService, audience, time.Now(), time.Minute)
		if err != nil {
			return status.Error(codes.Unauthenticated, "service identity unavailable")
		}
		ctx = metadata.AppendToOutgoingContext(ctx, identity.MetadataKey, token)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// UnaryServerServiceAuth authenticates a workload and optionally restricts
// which source services may call this destination.
func UnaryServerServiceAuth(secret, audience string, allowedSources map[string]struct{}) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		incoming, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "service identity is required")
		}
		values := incoming.Get(identity.MetadataKey)
		if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
			return nil, status.Error(codes.Unauthenticated, "service identity is required")
		}
		claims, err := identity.Verify(values[0], secret, audience, time.Now())
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid service identity")
		}
		if len(allowedSources) > 0 {
			if _, allowed := allowedSources[claims.Source]; !allowed {
				return nil, status.Error(codes.PermissionDenied, "service is not allowed to call destination")
			}
		}
		ctx = context.WithValue(ctx, serviceClaimsContextKey{}, claims)
		return handler(ctx, req)
	}
}

// ServiceClaims returns verified workload claims from a server context.
func ServiceClaims(ctx context.Context) (identity.Claims, bool) {
	claims, ok := ctx.Value(serviceClaimsContextKey{}).(identity.Claims)
	return claims, ok
}
