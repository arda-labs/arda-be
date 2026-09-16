package interceptors

import (
	"context"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryServerRecovery converts panics raised by unary handlers into Internal
// errors, logging the panic value, stack trace and method server-side. Panic
// details are never returned to the caller.
func UnaryServerRecovery(logger *slog.Logger) grpc.UnaryServerInterceptor {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				method := ""
				if info != nil {
					method = info.FullMethod
				}
				logger.Error("grpc panic recovered",
					"panic", rec,
					"stack", string(debug.Stack()),
					"method", method,
				)
				resp = nil
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}
