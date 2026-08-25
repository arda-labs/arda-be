package interceptors

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryServerServiceAuthAcceptsAllowedSource(t *testing.T) {
	secret := strings.Repeat("s", 32)
	token, err := identity.Issue(secret, "workflow-service", "crm-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identity.MetadataKey, token))
	interceptor := UnaryServerServiceAuth(secret, "crm-service", map[string]struct{}{"workflow-service": {}})
	_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Call"}, func(ctx context.Context, req any) (any, error) {
		if claims, ok := ServiceClaims(ctx); !ok || claims.Source != "workflow-service" {
			t.Fatalf("claims = %+v, ok=%v", claims, ok)
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnaryServerServiceAuthRejectsUnknownSource(t *testing.T) {
	secret := strings.Repeat("s", 32)
	token, err := identity.Issue(secret, "unknown-service", "crm-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(identity.MetadataKey, token))
	interceptor := UnaryServerServiceAuth(secret, "crm-service", map[string]struct{}{"workflow-service": {}})
	_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) { return nil, nil })
	if status.Code(err) != 7 { // PermissionDenied
		t.Fatalf("code = %v, want permission denied", status.Code(err))
	}
}
