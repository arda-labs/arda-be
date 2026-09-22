package service

import (
	"context"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// ListAgreementsForReporting is the statistical ETL read: the whole tenant
// drawdown slice (optionally one org) in the reporting projection.
func (s *LoanService) ListAgreementsForReporting(ctx context.Context, tenantID, orgCode string) ([]domain.ReportingAgreement, error) {
	items, err := s.repo.ListAgreementsForReporting(ctx, tenantID, orgCode)
	return items, mapRepoError(err)
}

// ListCollateralsForReporting is the statistical ETL read of the collateral
// register.
func (s *LoanService) ListCollateralsForReporting(ctx context.Context, tenantID string) ([]domain.ReportingCollateral, error) {
	items, err := s.repo.ListCollateralsForReporting(ctx, tenantID)
	return items, mapRepoError(err)
}
