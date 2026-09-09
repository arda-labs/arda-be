package interceptors

import (
	"context"
	"testing"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestUnaryServerMetadataPropagateCopiesTenant(t *testing.T) {
	incomingCtx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"x-request-id", "req-1",
		"x-tenant-id", "tenant-1",
		"x-user-id", "user-1",
	))
	interceptor := UnaryServerMetadataPropagate()
	var gotTenant, gotUser, gotRequestID string
	handler := func(ctx context.Context, _ any) (any, error) {
		out := ardametadata.FromOutgoing(ctx)
		gotTenant, gotUser, gotRequestID = out.TenantID, out.UserID, out.RequestID
		return nil, nil
	}
	if _, err := interceptor(incomingCtx, struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/test/Method"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if gotTenant != "tenant-1" || gotUser != "user-1" || gotRequestID != "req-1" {
		t.Fatalf("outgoing metadata not populated: tenant=%q user=%q requestID=%q", gotTenant, gotUser, gotRequestID)
	}
}

func TestUnaryServerMetadataPropagateEmptyIncoming(t *testing.T) {
	interceptor := UnaryServerMetadataPropagate()
	called := false
	handler := func(ctx context.Context, _ any) (any, error) {
		called = true
		return nil, nil
	}
	if _, err := interceptor(context.Background(), struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/test/Method"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not invoked")
	}
}
