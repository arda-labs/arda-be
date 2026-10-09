package service

import (
	"context"
	"errors"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	"log/slog"
	"math"
	"strings"
)

func (s *LoanService) ListContracts(ctx context.Context, tenantID, status, q string) ([]domain.Contract, error) {
	items, err := s.repo.ListContracts(ctx, tenantID, status, q)
	return items, mapRepoError(err)
}

// ListContractsPaged is the normalized contract list (SQL paging + sort): q
// ILIKEs contract_no/customer_code/contract_code, sort is a whitelist key
// (created_at | contract_no | loan_amt_minor) validated by the handler's
// ListSpec, and the total feeds the canonical list envelope.
func (s *LoanService) ListContractsPaged(ctx context.Context, tenantID, status, q, sort, order string, page, perPage int) ([]domain.Contract, int, error) {
	items, total, err := s.repo.ListContractsPaged(ctx, tenantID, repository.ContractListFilter{
		Status:  status,
		Search:  q,
		Sort:    sort,
		Order:   order,
		Page:    page,
		PerPage: perPage,
	})
	return items, total, mapRepoError(err)
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
	// Approval-tier limits for the BPMN GW_ApprovalLevel conditions
	// (amount > pgdLimit / amount > gdLimit): exact product row first, then
	// the org-wide row; no configured limit → sentinel MaxInt keeps the old
	// default-Execute-tier behavior (PGD review still runs) with a warn.
	pgdLimit, gdLimit := s.approvalLimits(ctx, tenantID, contract)
	vars := map[string]any{
		"contractId":   contract.ID,
		"contractCode": contract.ContractCode,
		"customerCode": contract.CustomerCode,
		"amount":       contract.LoanAmt,
		"fundSource":   "BRANCH",
		"pgdLimit":     pgdLimit,
		"gdLimit":      gdLimit,
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars, "lnm-contract-"+contract.ID+"-submit"); err != nil {
		return nil, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetContractWorkflowCase(ctx, tenantID, contract.ID, caseCreated.Id, caseCreated.GetCaseCode()); err != nil {
		return nil, mapRepoError(err)
	}
	item, err := s.repo.GetContract(ctx, tenantID, id)
	return &item, mapRepoError(err)
}

func (s *LoanService) SetContractStatus(ctx context.Context, tenantID, id, status, reason string) error {
	if status == "" {
		return ardaerrors.New(ardaerrors.CodeRequired, "status is required")
	}
	contract, err := s.repo.GetContract(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	if err := domain.CanTransition(domain.ContractMachine, domain.Status(contract.Status), domain.Status(status), reason); err != nil {
		return mapRepoError(err)
	}
	return mapRepoError(s.repo.UpdateContractStatus(ctx, tenantID, id, contract.Status, status))
}

// UpdateContract is the maker revise on the formation screen: only the
// editable whitelist fields are taken from the payload and only while the
// contract is still DRAFT, PENDING_APPROVAL, or REJECTED. A rejected edit
// returns the contract to DRAFT; APPROVED/DISBURSED/CLOSED contracts are frozen.
func (s *LoanService) UpdateContract(ctx context.Context, tenantID, id string, in *domain.Contract) (*domain.Contract, error) {
	if in == nil {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "request body is required")
	}
	if err := domain.ValidateContractUpdate(in); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	current, err := s.repo.GetContract(ctx, tenantID, id)
	if err = mapRepoError(err); err != nil {
		return nil, err
	}
	if !domain.ContractEditable(current.Status) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "contract_not_editable: only DRAFT, PENDING_APPROVAL, or REJECTED contracts can be revised")
	}
	patch := domain.Contract{
		ContractNo:           strings.TrimSpace(in.ContractNo),
		LoanAmt:              in.LoanAmt,
		InterestRate:         in.InterestRate,
		LoanTerm:             in.LoanTerm,
		TermUnit:             strings.TrimSpace(in.TermUnit),
		ContractDate:         in.ContractDate,
		MaturityDate:         in.MaturityDate,
		InterestScheduleDay:  in.InterestScheduleDay,
		InterestPaymentFreq:  strings.TrimSpace(in.InterestPaymentFreq),
		PrincipalPaymentFreq: strings.TrimSpace(in.PrincipalPaymentFreq),
		PurposeCode:          strings.TrimSpace(in.PurposeCode),
		EmployeeCode:         strings.TrimSpace(in.EmployeeCode),
		IndustryCode:         strings.TrimSpace(in.IndustryCode),
		LoanMethodCode:       strings.TrimSpace(in.LoanMethodCode),
	}
	item, err := s.repo.UpdateContract(ctx, tenantID, id, &patch)
	if err != nil {
		if errors.Is(err, repository.ErrContractNotEditable) {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "contract_not_editable: only DRAFT, PENDING_APPROVAL, or REJECTED contracts can be revised")
		}
		return nil, mapRepoError(err)
	}
	return item, nil
}

// approvalLimitSentinel is the no-config fallback for the BPMN approval-tier
// variables: MaxInt keeps every submission on the default Execute tier (the
// same behavior the hardcoded sentinels had before the limit table existed).
const approvalLimitSentinel = math.MaxInt64

// approvalLimits resolves the formation approval-tier limits for one
// contract: the exact product row first, then the org-wide fallback, then
// the sentinel (no config → default Execute tier, same behavior as the old
// hardcoded MaxInt variables, with a warn). The product/org precedence lives
// in PickApprovalLimit so it stays unit-testable without a database.
func (s *LoanService) approvalLimits(ctx context.Context, tenantID string, contract domain.Contract) (pgd, gd int64) {
	pgd, gd = approvalLimitSentinel, approvalLimitSentinel
	product, org, err := s.repo.GetApprovalLimits(ctx, tenantID, contract.OrgCode, contract.ProductCode)
	if err != nil {
		// A lookup failure must not block submission — keep the sentinel
		// behavior and surface the cause in the logs.
		slog.Warn("approval limit lookup failed — using sentinels", "org", contract.OrgCode, "err", err)
		return pgd, gd
	}
	picked := PickApprovalLimit(org, product)
	if picked.OrgCode == "" {
		if contract.OrgCode != "" {
			slog.Warn("no approval limit configured — using sentinels (default Execute tier)",
				"org", contract.OrgCode, "product", contract.ProductCode)
		}
		return pgd, gd
	}
	return picked.PGDLimitMinor, picked.GDLimitMinor
}
