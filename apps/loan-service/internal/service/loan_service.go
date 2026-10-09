package service

import (
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
)

// LoanService covers the core credit entities: contracts, disbursement
// agreements, repay plans, mortgages and collaterals.
type LoanService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
}

func NewLoanService(repo *repository.LoanRepository, workflow AdjustmentSubmitter) *LoanService {
	return &LoanService{repo: repo, workflow: workflow}
}
