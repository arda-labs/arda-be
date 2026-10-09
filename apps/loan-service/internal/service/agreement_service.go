package service

import (
	"context"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"strings"
)

func (s *LoanService) ListAgreements(ctx context.Context, tenantID, contractCode string) ([]domain.Agreement, error) {
	items, err := s.repo.ListAgreements(ctx, tenantID, contractCode)
	return items, mapRepoError(err)
}

func (s *LoanService) CreateAgreement(ctx context.Context, tenantID, createdBy string, in *domain.Agreement) (*domain.Agreement, error) {
	if strings.TrimSpace(in.ContractCode) == "" || strings.TrimSpace(in.AgreementCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "contract_code and agreement_code are required")
	}
	if in.DisburseAmt <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "disburse_amt must be positive")
	}
	in.ID = repository.NewID("agrt")
	in.TenantID = tenantID
	in.CreatedBy = createdBy
	// Outstanding is derived from settled drawdowns: a freshly created
	// agreement carries no debt until the REGISTER settlement bumps it.
	in.OutstandingAmt = 0
	return s.repo.CreateAgreement(ctx, in)
}

func (s *LoanService) ListRepayPlans(ctx context.Context, tenantID, contractCode, agreementCode string) ([]domain.RepayPlan, error) {
	items, err := s.repo.ListRepayPlans(ctx, tenantID, contractCode, agreementCode)
	return items, mapRepoError(err)
}

func (s *LoanService) ListMortgages(ctx context.Context, tenantID, q string) ([]domain.Mortgage, error) {
	items, err := s.repo.ListMortgages(ctx, tenantID, q)
	return items, mapRepoError(err)
}

func (s *LoanService) CreateMortgage(ctx context.Context, tenantID, createdBy string, in *domain.Mortgage) (*domain.Mortgage, error) {
	if strings.TrimSpace(in.MortgageCode) == "" || !codePattern.MatchString(in.MortgageCode) {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "mortgage_code is required")
	}
	in.ID = repository.NewID("mrtg")
	in.TenantID = tenantID
	in.Status = "ACTIVE"
	return s.repo.CreateMortgage(ctx, in)
}

func (s *LoanService) ListCollaterals(ctx context.Context, tenantID, mortgageCode, q string) ([]domain.Collateral, error) {
	items, err := s.repo.ListCollaterals(ctx, tenantID, mortgageCode, q)
	return items, mapRepoError(err)
}

func (s *LoanService) CreateCollateral(ctx context.Context, tenantID, createdBy string, in *domain.Collateral) (*domain.Collateral, error) {
	if strings.TrimSpace(in.CollCode) == "" || !codePattern.MatchString(in.CollCode) {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "coll_code is required")
	}
	if in.DeductionRatio == nil {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "deduction_ratio is required")
	}
	if *in.DeductionRatio < 0 || *in.DeductionRatio > 100 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "deduction_ratio must be between 0 and 100")
	}
	in.ID = repository.NewID("coll")
	in.TenantID = tenantID
	in.Status = "ACTIVE"
	return s.repo.CreateCollateral(ctx, in)
}

func (s *LoanService) ListContractCollaterals(ctx context.Context, tenantID, contractCode string) ([]domain.ContractCollateral, error) {
	items, err := s.repo.ListContractCollaterals(ctx, tenantID, contractCode)
	return items, mapRepoError(err)
}

func (s *LoanService) AttachContractCollateral(ctx context.Context, tenantID string, in *domain.ContractCollateral) (*domain.ContractCollateral, error) {
	if strings.TrimSpace(in.ContractCode) == "" || strings.TrimSpace(in.CollCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "contract_code and coll_code are required")
	}
	in.ID = repository.NewID("ccol")
	in.TenantID = tenantID
	return s.repo.AttachContractCollateral(ctx, in)
}
