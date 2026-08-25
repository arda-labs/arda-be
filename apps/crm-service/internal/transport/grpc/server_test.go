package grpc

import (
	"context"
	"testing"

	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	"google.golang.org/grpc/metadata"
)

func TestCustomerScopeRequiresTenantAndOrganization(t *testing.T) {
	if _, err := customerScope(context.Background()); err == nil {
		t.Fatal("expected missing scope to fail")
	}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		ardametadata.TenantID, "tenant-1",
		ardametadata.OrgID, "org-1",
	))
	scope, err := customerScope(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if scope.TenantID != "tenant-1" || len(scope.OrgIDs) != 1 || scope.OrgIDs[0] != "org-1" {
		t.Fatalf("unexpected scope: %+v", scope)
	}
}
