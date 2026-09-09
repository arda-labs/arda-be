package service

import "github.com/arda-labs/arda/apps/loan-service/internal/repository"

// PickApprovalLimit resolves which lnm_approval_limits row feeds the
// formation approval-tier variables: the exact product row when present,
// otherwise the org-wide fallback row (product_code = ”). Passing
// product=nil (no product row found) selects the org-wide row; org=nil or
// both nil returns the sentinel-carrying empty struct so callers keep the
// default-Execute-tier behavior. Pure function — unit-tested without a DB.
func PickApprovalLimit(org, product *repository.ApprovalLimit) repository.ApprovalLimit {
	if product != nil {
		return *product
	}
	if org != nil {
		return *org
	}
	return repository.ApprovalLimit{}
}
