package interceptors

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUnaryServerRecoveryConvertsPanicToInternal(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	interceptor := UnaryServerRecovery(logger)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Call"}

	resp, err := interceptor(context.Background(), nil, info, func(context.Context, any) (any, error) {
		panic("boom")
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want %v", status.Code(err), codes.Internal)
	}
	if got := status.Convert(err).Message(); got != "internal error" {
		t.Fatalf("message = %q, want %q", got, "internal error")
	}
	if resp != nil {
		t.Fatalf("resp = %v, want nil", resp)
	}
}

func TestUnaryServerRecoveryPassesThroughHandlerResult(t *testing.T) {
	interceptor := UnaryServerRecovery(nil)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Call"}

	resp, err := interceptor(context.Background(), "request", info, func(context.Context, any) (any, error) {
		return "response", nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if resp != "response" {
		t.Fatalf("resp = %v, want response", resp)
	}
}

func TestUnaryServerRecoveryPassesThroughHandlerError(t *testing.T) {
	interceptor := UnaryServerRecovery(nil)
	wantErr := status.Error(codes.NotFound, "missing")

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Call"}, func(context.Context, any) (any, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
