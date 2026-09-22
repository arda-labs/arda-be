package service

import (
	"context"

	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
)

// ListContractsForReporting is the statistical ETL read of the fund-contract
// register.
func (s *CapitalService) ListContractsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.ReportingCapitalContract, error) {
	return s.repo.ListContractsForReporting(ctx, tenantID, orgCode)
}

// ListMovementsForReporting is the statistical ETL read of the fund movements.
func (s *CapitalService) ListMovementsForReporting(ctx context.Context, tenantID string) ([]repository.ReportingCapitalMovement, error) {
	return s.repo.ListMovementsForReporting(ctx, tenantID)
}
