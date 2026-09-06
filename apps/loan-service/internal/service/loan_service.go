package service

import (
	"context"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
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

func (s *LoanService) ListContracts(ctx context.Context, tenantID, status, q string) ([]domain.Contract, error) {
	items, err := s.repo.ListContracts(ctx, tenantID, status, q)
	return items, mapRepoError(err)
}

func (s *LoanService) GetContract(ctx context.Context, tenantID, id string) (domain.Contract, error) {
	item, err := s.repo.GetContract(ctx, tenantID, id)
	return item, mapRepoError(err)
}

// CreateContract registers a DRAFT contract; SubmitContract pushes it into
// the LOAN_FORMATION_V2 multi-level case.
func (s *LoanService) CreateContract(ctx context.Context, tenantID, createdBy string, in *domain.Contract) (*domain.Contract, error) {
	if strings.TrimSpace(in.ContractCode) == "" || !codePattern.MatchString(in.ContractCode) {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "contract_code is required")
	}
	if strings.TrimSpace(in.CustomerCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "customer_code is required")
	}
	if in.LoanAmt <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "loan_amt must be positive")
	}
	in.ID = repository.NewID("ctrt")
	in.TenantID = tenantID
	in.Status = domain.ContractDraft
	in.CreatedBy = createdBy
	return s.repo.CreateContract(ctx, in)
}

func (s *LoanService) SubmitContract(ctx context.Context, tenantID, actor, id string) (*domain.Contract, error) {
	if s.workflow == nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	contract, err := s.repo.GetContract(ctx, tenantID, id)
	if err = mapRepoError(err); err != nil {
		return nil, err
	}
	if contract.Status != domain.ContractDraft {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "only DRAFT contracts can be submitted")
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          "LOAN_FORMATION_V2",
		Title:             "Hình thành khoản vay — " + contract.ContractCode,
		PrimaryObjectType: "lnm.contract",
		PrimaryObjectID:   contract.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    "lnm-contract-" + contract.ID,
	})
	if err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	vars := map[string]any{
		"contractId":   contract.ID,
		"contractCode": contract.ContractCode,
		"customerCode": contract.CustomerCode,
		"amount":       contract.LoanAmt,
		"fundSource":   "BRANCH",
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars, "lnm-contract-"+contract.ID+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetContractWorkflowCase(ctx, tenantID, contract.ID, caseCreated.Id); err != nil {
		return nil, mapRepoError(err)
	}
	item, err := s.repo.GetContract(ctx, tenantID, id)
	return &item, mapRepoError(err)
}

func (s *LoanService) SetContractStatus(ctx context.Context, tenantID, id, status string) error {
	if status == "" {
		return ardaerrors.New(ardaerrors.CodeRequired, "status is required")
	}
	return mapRepoError(s.repo.UpdateContractStatus(ctx, tenantID, id, status))
}

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
	in.OutstandingAmt = in.DisburseAmt
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
