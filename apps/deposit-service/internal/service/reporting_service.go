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

// ListIBMDepositsForReporting is the statistical ETL read for the interbank
// deposit indicators (PCF topic "Tiền gửi TCTD").
func (s *SettlementService) ListIBMDepositsForReporting(ctx context.Context, tenantID, orgCode string) ([]repository.IBMReportingRow, error) {
	return s.repo.ListIBMDepositsForReporting(ctx, tenantID, orgCode)
}
