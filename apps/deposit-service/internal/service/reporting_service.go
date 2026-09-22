package service

import (
	"context"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
)

// ListSavingsForReporting is the statistical ETL read: the whole tenant savings
// slice (optionally one org) in the reporting projection.
func (s *SettlementService) ListSavingsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.Savings, error) {
	return s.repo.ListSavingsForReporting(ctx, tenantID, orgCode)
}
