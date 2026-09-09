package service

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
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

func (s *LoanService) SetContractStatus(ctx context.Context, tenantID, id, status string) error {
	if status == "" {
		return ardaerrors.New(ardaerrors.CodeRequired, "status is required")
	}
	return mapRepoError(s.repo.UpdateContractStatus(ctx, tenantID, id, status))
}

// contractEditableStatus reports whether a contract in this status can still
// be revised by the maker (mirror of the DRAFT/PENDING WHERE clause in
// UpdateContract — the SQL guard stays as the race backstop).
func contractEditableStatus(status string) bool {
	return status == domain.ContractDraft || status == domain.ContractPending
}

// validateContractUpdate checks the editable payload of the maker revise
// (mirror CreateContract's amount/rate/term rules; dates are optional but
// must be a real ISO calendar date when present).
func validateContractUpdate(in *domain.Contract) error {
	if in.LoanAmt <= 0 {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "loan_amt must be positive")
	}
	if in.InterestRate <= 0 {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "interest_rate must be positive")
	}
	if in.LoanTerm <= 0 {
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "loan_term must be positive")
	}
	for field, date := range map[string]string{"contract_date": in.ContractDate, "maturity_date": in.MaturityDate} {
		if date == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return ardaerrors.New(ardaerrors.CodeInvalidInput, field+" must be a valid YYYY-MM-DD date")
		}
	}
	return nil
}

// UpdateContract is the maker revise on the formation screen: only the
// editable whitelist fields are taken from the payload and only while the
// contract is still DRAFT or PENDING (checker-decided or active contracts
// are frozen — REJECTED/ACTIVE/CLOSED reject with contract_not_editable).
func (s *LoanService) UpdateContract(ctx context.Context, tenantID, id string, in *domain.Contract) (*domain.Contract, error) {
	if in == nil {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "request body is required")
	}
	if err := validateContractUpdate(in); err != nil {
		return nil, err
	}
	current, err := s.repo.GetContract(ctx, tenantID, id)
	if err = mapRepoError(err); err != nil {
		return nil, err
	}
	if !contractEditableStatus(current.Status) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "contract_not_editable: only DRAFT or PENDING contracts can be revised")
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
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "contract_not_editable: only DRAFT or PENDING contracts can be revised")
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

// ── Products ──

var productTypes = map[string]bool{"TERM": true, "LIMIT": true}

func (s *LoanService) ListProducts(ctx context.Context, tenantID string, includeInactive bool, isActive, q, sort, order string) ([]domain.LoanProduct, error) {
	items, err := s.repo.ListProducts(ctx, tenantID, includeInactive, isActive, q, sort, order)
	return items, mapRepoError(err)
}

func (s *LoanService) UpsertProduct(ctx context.Context, tenantID, createdBy string, in *domain.LoanProduct) (*domain.LoanProduct, error) {
	if strings.TrimSpace(in.Code) == "" || !codePattern.MatchString(in.Code) {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "product_code is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "name is required")
	}
	if !productTypes[in.ProductType] {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "product_type must be TERM or LIMIT")
	}
	if in.ProductType == "" {
		in.ProductType = "TERM"
	}
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	if in.TermUnit == "" {
		in.TermUnit = "MONTH"
	}
	in.ID = repository.NewID("prd")
	in.TenantID = tenantID
	in.CreatedBy = createdBy
	return s.repo.UpsertProduct(ctx, in)
}

// ── VFU (ủy thác) ──

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
