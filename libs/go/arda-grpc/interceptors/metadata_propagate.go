package interceptors

import (
	"context"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"google.golang.org/grpc"
)

// UnaryServerMetadataPropagate copies the caller's metadata from the incoming
// context into the outgoing context. Repository helpers read tenant context via
// ardametadata.FromOutgoing, which ardametadata.HTTPMiddleware seeds for HTTP
// handlers — without this bridge, gRPC handlers see an empty outgoing context
// and "verified tenant scope is required" fails every repo call.
func UnaryServerMetadataPropagate() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ardametadata.AppendToOutgoing(ctx, ardametadata.FromIncoming(ctx)), req)
	}
}
