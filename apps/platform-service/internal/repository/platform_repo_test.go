package repository

import "testing"

func TestRequireTenantID(t *testing.T) {
	for _, tenantID := range []string{"", " ", "\t"} {
		if err := requireTenantID(tenantID); err == nil {
			t.Fatalf("requireTenantID(%q) accepted an empty scope", tenantID)
		}
	}
	if err := requireTenantID("tenant-1"); err != nil {
		t.Fatalf("requireTenantID rejected a valid scope: %v", err)
	}
}
