package service

import (
	"math"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

// TestPickApprovalLimitPrecedence covers the lnm_approval_limits lookup
// precedence: exact product row > org-wide row > sentinel (no config). The
// SQL side narrows to (product, org) rows via product_code IN ($3, ”); this
// test pins the Go-side pick so the fallback order cannot silently flip.
func TestPickApprovalLimitPrecedence(t *testing.T) {
	orgRow := &repository.ApprovalLimit{OrgCode: "LNM_PGD", ProductCode: "", PGDLimitMinor: 50000000000, GDLimitMinor: 200000000000}
	productRow := &repository.ApprovalLimit{OrgCode: "LNM_PGD", ProductCode: "TERM06", PGDLimitMinor: 100000000000, GDLimitMinor: 400000000000}

	// Product row wins over the org-wide row.
	picked := PickApprovalLimit(orgRow, productRow)
	if picked.ProductCode != "TERM06" || picked.PGDLimitMinor != 100000000000 || picked.GDLimitMinor != 400000000000 {
		t.Fatalf("product row must win: %+v", picked)
	}

	// No product row → org-wide fallback.
	picked = PickApprovalLimit(orgRow, nil)
	if picked.ProductCode != "" || picked.PGDLimitMinor != 50000000000 || picked.GDLimitMinor != 200000000000 {
		t.Fatalf("org-wide fallback expected: %+v", picked)
	}

	// No row at all → empty struct (caller maps it to the sentinel).
	picked = PickApprovalLimit(nil, nil)
	if picked.ProductCode != "" || picked.OrgCode != "" {
		t.Fatalf("no row must yield the zero struct: %+v", picked)
	}

	// The sentinel keeps the historical default-Execute-tier behavior.
	if approvalLimitSentinel != math.MaxInt64 {
		t.Fatalf("approvalLimitSentinel = %d, want MaxInt64", approvalLimitSentinel)
	}
}
