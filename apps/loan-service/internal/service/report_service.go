package service

import (
	"context"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

// LoanReportService serves the loan report reads (W4c, data owner computes).
type LoanReportService struct {
	repo *repository.LoanRepository
}

func NewLoanReportService(repo *repository.LoanRepository) *LoanReportService {
	return &LoanReportService{repo: repo}
}

// LoanLedger is the sổ/sao kê khoản vay report.
func (s *LoanReportService) LoanLedger(ctx context.Context, tenantID, fromDate, toDate, contractCode string) ([]repository.LoanLedgerRow, error) {
	return s.repo.LoanLedger(ctx, tenantID, fromDate, toDate, contractCode)
}

// LoanStatement is the khoản vay statement report.
func (s *LoanReportService) LoanStatement(ctx context.Context, tenantID, contractCode string) ([]repository.LoanStatementRow, error) {
	return s.repo.LoanStatement(ctx, tenantID, contractCode)
}

// CollateralStatement is the sổ tài sản bảo đảm report.
func (s *LoanReportService) CollateralStatement(ctx context.Context, tenantID, status string) ([]repository.CollateralStatementRow, error) {
	return s.repo.CollateralStatement(ctx, tenantID, status)
}
