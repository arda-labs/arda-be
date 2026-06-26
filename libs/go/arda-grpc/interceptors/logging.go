package interceptors

import (
	"context"
	"log/slog"
	"time"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func UnaryServerLogging(logger *slog.Logger) grpc.UnaryServerInterceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		md := ardametadata.FromIncoming(ctx)
		logger.Info("grpc request",
			"method", info.FullMethod,
			"code", code.String(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", md.RequestID,
			"tenant_id", md.TenantID,
			"user_id", md.UserID,
			"source_service", md.SourceService,
		)
		return resp, err
	}
}

func UnaryClientMetadata(sourceService string, base ardametadata.Context) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req any, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		out := ardametadata.FromOutgoing(ctx)
		if out.RequestID == "" {
			out.RequestID = base.RequestID
		}
		if out.TenantID == "" {
			out.TenantID = base.TenantID
		}
		if out.Locale == "" {
			out.Locale = base.Locale
		}
		if out.SourceService == "" {
			out.SourceService = sourceService
		}
		ctx = ardametadata.AppendToOutgoing(ctx, out)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
