package service

import (
	"context"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
)

// ReportService serves the DPM/IBM report reads (W4c, pattern Q8: data owner
// computes; FE renders + exports).
type ReportService struct {
	repo *repository.DepositRepository
}

func NewReportService(repo *repository.DepositRepository) *ReportService {
	return &ReportService{repo: repo}
}

// SavingsStatement is the sổ tiền gửi report.
func (s *ReportService) SavingsStatement(ctx context.Context, tenantID, fromDate, toDate, status string) ([]repository.SavingsStatementRow, error) {
	return s.repo.SavingsStatement(ctx, tenantID, fromDate, toDate, status)
}

// SavingsTransactions is the giao dịch tiền gửi report.
func (s *ReportService) SavingsTransactions(ctx context.Context, tenantID, fromDate, toDate, txnType string) ([]repository.SavingsTxnRow, error) {
	return s.repo.SavingsTransactions(ctx, tenantID, fromDate, toDate, txnType)
}

// InterbankStatement is the sổ tiền gửi liên ngân hàng report.
func (s *ReportService) InterbankStatement(ctx context.Context, tenantID, fromDate, toDate, status string) ([]repository.InterbankStatementRow, error) {
	return s.repo.InterbankStatement(ctx, tenantID, fromDate, toDate, status)
}

// InterbankTransactions is the giao dịch liên ngân hàng report.
func (s *ReportService) InterbankTransactions(ctx context.Context, tenantID, fromDate, toDate string) ([]repository.InterbankTxnRow, error) {
	return s.repo.InterbankTransactions(ctx, tenantID, fromDate, toDate)
}
