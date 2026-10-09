package grpc

import (
	"testing"

	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
)

func TestBusinessDateScopeMapsHeadOfficeToGlobalSystem(t *testing.T) {
	scope, err := businessDateScope("tenant-dev", &platformv1.ScopeSelector{ScopeType: "system"})
	if err != nil {
		t.Fatal(err)
	}
	if scope.Type != ardaBusinessDate.ScopeSystem || scope.TenantID != "tenant-dev" || scope.OrgCode != "" {
		t.Fatalf("business date scope = %+v", scope)
	}
}

func TestBusinessDateScopeParsesOrgScopeForExplicitMapping(t *testing.T) {
	_, err := businessDateScope("tenant-dev", &platformv1.ScopeSelector{ScopeType: "org", ScopeId: "BR-01"})
	if err != nil {
		t.Fatal(err)
	}
	// Scope parsing is valid; the repository fails closed until a tenant/org
	// mapping is explicitly approved and migrated.
}

func TestBusinessDateScopeRequiresVerifiedTenant(t *testing.T) {
	if _, err := verifiedTenant(t.Context(), ""); err == nil {
		t.Fatal("expected missing verified tenant to be rejected")
	}
}
