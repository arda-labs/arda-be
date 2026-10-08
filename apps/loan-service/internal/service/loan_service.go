package service

import (
	"context"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"strings"
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

func (s *LoanService) ListVfuParties(ctx context.Context, tenantID, q string) ([]domain.VfuParty, error) {
	items, err := s.repo.ListVfuParties(ctx, tenantID, q)
	return items, mapRepoError(err)
}

func (s *LoanService) CreateVfuParty(ctx context.Context, tenantID, createdBy string, in *domain.VfuParty) (*domain.VfuParty, error) {
	if strings.TrimSpace(in.PartyCode) == "" || !codePattern.MatchString(in.PartyCode) {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "party_code is required")
	}
	if strings.TrimSpace(in.PartyName) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "party_name is required")
	}
	in.ID = repository.NewID("vparty")
	in.TenantID = tenantID
	in.Status = "ACTIVE"
	in.CreatedBy = createdBy
	return s.repo.CreateVfuParty(ctx, in)
}

func (s *LoanService) ListVfuMandates(ctx context.Context, tenantID, q string) ([]domain.VfuMandate, error) {
	items, err := s.repo.ListVfuMandates(ctx, tenantID, q)
	return items, mapRepoError(err)
}

func (s *LoanService) CreateVfuMandate(ctx context.Context, tenantID, createdBy string, in *domain.VfuMandate) (*domain.VfuMandate, error) {
	if strings.TrimSpace(in.MandateCode) == "" || !codePattern.MatchString(in.MandateCode) {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "mandate_code is required")
	}
	if strings.TrimSpace(in.PartyCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "party_code is required")
	}
	in.ID = repository.NewID("vmand")
	in.TenantID = tenantID
	in.Status = "ACTIVE"
	in.CreatedBy = createdBy
	return s.repo.CreateVfuMandate(ctx, in)
}

func (s *LoanService) ListVfuPlans(ctx context.Context, tenantID, mandateCode string) ([]domain.VfuPlan, error) {
	items, err := s.repo.ListVfuPlans(ctx, tenantID, mandateCode)
	return items, mapRepoError(err)
}

func (s *LoanService) CreateVfuPlan(ctx context.Context, tenantID, createdBy string, in *domain.VfuPlan) (*domain.VfuPlan, error) {
	if strings.TrimSpace(in.PlanCode) == "" || !codePattern.MatchString(in.PlanCode) {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "plan_code is required")
	}
	if strings.TrimSpace(in.MandateCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "mandate_code is required")
	}
	in.ID = repository.NewID("vplan")
	in.TenantID = tenantID
	in.Status = "ACTIVE"
	in.CreatedBy = createdBy
	return s.repo.CreateVfuPlan(ctx, in)
}

// UpdateVfuParty edits a trust party (code immutable).
func (s *LoanService) UpdateVfuParty(ctx context.Context, tenantID string, in *domain.VfuParty) (*domain.VfuParty, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "id is required")
	}
	if strings.TrimSpace(in.PartyName) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "party_name is required")
	}
	in.TenantID = tenantID
	out, err := s.repo.UpdateVfuParty(ctx, in)
	return out, mapRepoError(err)
}

// UpdateVfuMandate edits a trust mandate (code immutable).
func (s *LoanService) UpdateVfuMandate(ctx context.Context, tenantID string, in *domain.VfuMandate) (*domain.VfuMandate, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "id is required")
	}
	if strings.TrimSpace(in.PartyCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "party_code is required")
	}
	in.TenantID = tenantID
	out, err := s.repo.UpdateVfuMandate(ctx, in)
	return out, mapRepoError(err)
}

// UpdateVfuPlan edits a funding plan (code immutable).
func (s *LoanService) UpdateVfuPlan(ctx context.Context, tenantID string, in *domain.VfuPlan) (*domain.VfuPlan, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "id is required")
	}
	if strings.TrimSpace(in.MandateCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "mandate_code is required")
	}
	in.TenantID = tenantID
	out, err := s.repo.UpdateVfuPlan(ctx, in)
	return out, mapRepoError(err)
}

// Dossier builds the composite view for one contract.
func (s *LoanService) Dossier(ctx context.Context, tenantID, contractID string) (*repository.Dossier, error) {
	contract, err := s.repo.GetContract(ctx, tenantID, contractID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	agreements, err := s.repo.ListAgreements(ctx, tenantID, contract.ContractCode)
	if err != nil {
		return nil, mapRepoError(err)
	}
	plans, err := s.repo.ListRepayPlans(ctx, tenantID, contract.ContractCode, "")
	if err != nil {
		return nil, mapRepoError(err)
	}
	disbursements, _, err := s.repo.ListDisbursements(ctx, tenantID, nil, "", contract.ContractCode, "", "", "", "", 500, 0)
	if err != nil {
		return nil, mapRepoError(err)
	}
	collections, _, err := s.repo.ListCollections(ctx, tenantID, nil, "", contract.ContractCode, "", "", "", 500, 0)
	if err != nil {
		return nil, mapRepoError(err)
	}
	mortgages, err := s.repo.ListMortgages(ctx, tenantID, contract.ContractCode)
	if err != nil {
		return nil, mapRepoError(err)
	}
	var collaterals []domain.Collateral
	for _, m := range mortgages {
		rows, err := s.repo.ListCollaterals(ctx, tenantID, m.MortgageCode, "")
		if err != nil {
			return nil, mapRepoError(err)
		}
		collaterals = append(collaterals, rows...)
	}
	caseIDs, err := s.repo.ListContractCaseIDs(ctx, tenantID, contract.ID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return &repository.Dossier{
		Contract:      contract,
		Agreements:    agreements,
		RepayPlans:    plans,
		Disbursements: disbursements,
		Collections:   collections,
		Mortgages:     mortgages,
		Collaterals:   collaterals,
		CaseIDs:       caseIDs,
	}, nil
}

// OperationMetrics returns the TT92 performance aggregates finance-service
// reads for PLIIb: collection volume in [fromDate, toDate] and the current
// non-performing balance (debt groups 3-5).
func (s *LoanService) OperationMetrics(ctx context.Context, tenantID, fromDate, toDate string) (int64, int64, error) {
	volume, err := s.repo.CollectionVolume(ctx, tenantID, fromDate, toDate)
	if err != nil {
		return 0, 0, err
	}
	npl, err := s.repo.NPLBalance(ctx, tenantID)
	if err != nil {
		return 0, 0, err
	}
	return volume, npl, nil
}
