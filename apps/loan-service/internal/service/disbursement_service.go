package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
)

// DisbursementService runs the two-phase LNM.300.02 drawdown flow: create
// DRAFT → submit case (LNM_DISB_REGISTER_V2 or LNM_DISB_COMPLETE_V2) →
// workflow approval → the posting steps reserve/post through the finance
// PostingService two-phase lifecycle (worker side).
type DisbursementService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
}

func NewDisbursementService(repo *repository.LoanRepository, workflow AdjustmentSubmitter) *DisbursementService {
	return &DisbursementService{repo: repo, workflow: workflow}
}

// BPMN case types for the two disbursement flows.
const (
	RegisterCaseType = "LNM_DISB_REGISTER_V2"
	CompleteCaseType = "LNM_DISB_COMPLETE_V2"
)

// List returns one page of the disbursement ledger. q matches the
// agreement/contract codes (ILIKE), status/contract_code/flow_type are the
// whitelisted exact filters, sort/order are the whitelisted keys validated by
// the handler's ListSpec.
func (s *DisbursementService) List(ctx context.Context, tenantID string, orgCodes []string, status, contractCode, flowType, q, sort, order string, page, perPage int) ([]domain.Disbursement, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	items, total, err := s.repo.ListDisbursements(ctx, tenantID, orgCodes, status, contractCode, flowType, q, sort, order, perPage, (page-1)*perPage)
	return items, total, mapRepoError(err)
}

func (s *DisbursementService) Get(ctx context.Context, tenantID, id string) (domain.Disbursement, error) {
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return domain.Disbursement{}, mapRepoError(err)
	}
	return *item, nil
}

// Create registers a DRAFT disbursement. REGISTER requires the agreement +
// contract to exist and the amount to fit the contract headroom (loan amount
// minus what its agreements already carry as outstanding). COMPLETE requires
// a POSTED REGISTER source and the amount to fit the source's un-completed
// remainder. Amounts are int64 minor units.
func (s *DisbursementService) Create(ctx context.Context, tenantID, createdBy string, in *domain.Disbursement) (*domain.Disbursement, error) {
	if in == nil || strings.TrimSpace(in.ContractCode) == "" || strings.TrimSpace(in.AgreementCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "contract_code and agreement_code are required")
	}
	if in.DisburseAmtMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "disburse_amt_minor must be positive")
	}
	if !isValidISODate(in.DisburseDate) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "disburse_date must be YYYY-MM-DD")
	}
	if in.FlowType == "" {
		in.FlowType = domain.FlowRegister
	}
	if _, err := s.repo.GetAgreementByCode(ctx, tenantID, in.AgreementCode); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "agreement_code not found: "+in.AgreementCode)
	}
	switch in.FlowType {
	case domain.FlowRegister:
		contract, err := s.repo.GetContractByCode(ctx, tenantID, strings.TrimSpace(in.ContractCode))
		if err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "contract_code not found: "+in.ContractCode)
		}
		outstanding, err := s.repo.SumAgreementOutstanding(ctx, tenantID, contract.ContractCode)
		if err != nil {
			return nil, mapRepoError(err)
		}
		if err := checkRegisterLimit(contract.LoanAmt, outstanding, in.DisburseAmtMinor); err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
		}
	case domain.FlowComplete:
		source, err := s.validateCompleteSource(ctx, tenantID, strings.TrimSpace(in.SourceRegisterID))
		if err != nil {
			return nil, err
		}
		completed, err := s.repo.SumCompleteForSource(ctx, tenantID, source.ID)
		if err != nil {
			return nil, mapRepoError(err)
		}
		if err := checkCompleteRemainder(source.DisburseAmtMinor, completed, in.DisburseAmtMinor); err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
		}
		in.SourceRegisterID = source.ID
	default:
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "flow_type must be REGISTER or COMPLETE")
	}
	in.ID = repository.NewID("disb")
	in.TenantID = tenantID
	in.Status = domain.DisbursementDraft
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	in.CreatedBy = createdBy
	created, err := s.repo.CreateDisbursement(ctx, in)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return created, nil
}

// validateCompleteSource guards the COMPLETE flow against its REGISTER
// source: it must exist in the same tenant and be a POSTED REGISTER.
func (s *DisbursementService) validateCompleteSource(ctx context.Context, tenantID, sourceID string) (*domain.Disbursement, error) {
	if sourceID == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "source_register_id is required for COMPLETE disbursements")
	}
	source, err := s.repo.GetDisbursement(ctx, tenantID, sourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "source_register_id not found: "+sourceID)
		}
		return nil, mapRepoError(err)
	}
	if source.FlowType != domain.FlowRegister {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "source_register_id must reference a REGISTER disbursement")
	}
	if source.Status != domain.DisbursementPosted {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "source register is not POSTED yet")
	}
	return source, nil
}

// checkRegisterLimit is the over-limit guard: one contract's drawdowns may
// never exceed its loan amount, measured by the outstanding its agreements
// already carry (the settled share of the headroom).
func checkRegisterLimit(contractLoanAmtMinor, contractOutstandingMinor, disburseAmtMinor int64) error {
	if disburseAmtMinor > contractLoanAmtMinor-contractOutstandingMinor {
		return fmt.Errorf("disburse_amt_minor %d exceeds contract headroom %d (loan_amt_minor %d - outstanding_amt_minor %d)",
			disburseAmtMinor, contractLoanAmtMinor-contractOutstandingMinor, contractLoanAmtMinor, contractOutstandingMinor)
	}
	return nil
}

// checkCompleteRemainder guards the COMPLETE flow: completions of one source
// register may never exceed the register amount (in-flight cases count).
func checkCompleteRemainder(sourceRegisterAmtMinor, completedAmtMinor, disburseAmtMinor int64) error {
	if disburseAmtMinor > sourceRegisterAmtMinor-completedAmtMinor {
		return fmt.Errorf("disburse_amt_minor %d exceeds source remainder %d (register %d - completed %d)",
			disburseAmtMinor, sourceRegisterAmtMinor-completedAmtMinor, sourceRegisterAmtMinor, completedAmtMinor)
	}
	return nil
}

// Submit pushes the DRAFT disbursement into its flow's case (register or
// complete, by flow_type).
func (s *DisbursementService) Submit(ctx context.Context, tenantID, actor, id string) (domain.Disbursement, error) {
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return domain.Disbursement{}, mapRepoError(err)
	}
	if item.Status != domain.DisbursementDraft {
		return domain.Disbursement{}, ardaerrors.New(ardaerrors.CodeInvalidInput, "only DRAFT disbursements can be submitted")
	}
	if s.workflow == nil {
		return domain.Disbursement{}, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	flowType := item.FlowType
	if flowType == "" {
		flowType = domain.FlowRegister
	}
	caseType := RegisterCaseType
	if flowType == domain.FlowComplete {
		caseType = CompleteCaseType
	}
	title := "Giải ngân — " + item.ContractCode + " / " + item.AgreementCode
	if flowType == domain.FlowComplete {
		title = "Hoàn tất giải ngân — " + item.ContractCode + " / " + item.AgreementCode
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          caseType,
		CaseCode:          "",
		Title:             title,
		PrimaryObjectType: "lnm.disbursement",
		PrimaryObjectID:   item.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("lnm-disbursement-%s-%s", strings.ToLower(flowType), item.ID),
	})
	if err != nil {
		return domain.Disbursement{}, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	vars := map[string]any{
		"disbursementId": item.ID,
		"contractCode":   item.ContractCode,
		"agreementCode":  item.AgreementCode,
		"disburseDate":   item.DisburseDate,
		"flowType":       flowType,
	}
	if item.SourceRegisterID != "" {
		vars["sourceRegisterId"] = item.SourceRegisterID
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars, fmt.Sprintf("lnm-disbursement-%s-%s-submit", strings.ToLower(flowType), item.ID)); err != nil {
		return domain.Disbursement{}, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetDisbursementCaseAndJournal(ctx, tenantID, item.ID, caseCreated.Id, ""); err != nil {
		return domain.Disbursement{}, mapRepoError(err)
	}
	if err := s.repo.SetDisbursementStatus(ctx, tenantID, item.ID, domain.DisbursementSubmitted, actor); err != nil {
		return domain.Disbursement{}, mapRepoError(err)
	}
	updated, err := s.repo.GetDisbursement(ctx, tenantID, item.ID)
	if err != nil {
		return domain.Disbursement{}, mapRepoError(err)
	}
	return *updated, nil
}

// Check validates the disbursement is actionable (BPMN validate job).
func (s *DisbursementService) Check(ctx context.Context, tenantID, id string) (bool, string, error) {
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return false, mapRepoError(err).Error(), nil
	}
	if item.Status != domain.DisbursementSubmitted {
		return false, fmt.Sprintf("status %s is not actionable", item.Status), nil
	}
	return true, "", nil
}

// Resolve applies the workflow decision. REJECT/CANCEL are terminal without
// posting — the finance hold release is the workflow cancel worker's job.
func (s *DisbursementService) Resolve(ctx context.Context, tenantID, id, decision, decidedBy, note string) error {
	switch decision {
	case "APPROVE":
		if err := s.repo.SetDisbursementStatus(ctx, tenantID, id, domain.DisbursementApproved, decidedBy); err != nil {
			return mapRepoError(err)
		}
	case "REJECT", "CANCEL":
		if err := s.repo.SetDisbursementStatus(ctx, tenantID, id, domain.DisbursementRejected, decidedBy); err != nil {
			return mapRepoError(err)
		}
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "unknown decision "+decision)
	}
	return nil
}

// Settle marks POSTED with the journal entry id and applies the flow side
// effect — dispatched by the disbursement's stored flow_type (worker RPC
// surface; the worker already knows its flow and posts the matching legs).
func (s *DisbursementService) Settle(ctx context.Context, tenantID, id, journalEntryID, actor string) error {
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	flowType := item.FlowType
	if flowType == "" {
		flowType = domain.FlowRegister
	}
	if flowType == domain.FlowComplete {
		return s.SettleComplete(ctx, tenantID, item.ID, journalEntryID, actor)
	}
	return s.SettleRegister(ctx, tenantID, item.ID, journalEntryID, actor)
}

// SettleRegister marks the REGISTER leg POSTED and settles the agreement:
// outstanding + pending (in-transit) both bump by the drawdown amount, and a
// PENDING agreement goes ACTIVE. Contract status is deliberately left to the
// COMPLETE settle (EPAS first-completion semantics).
func (s *DisbursementService) SettleRegister(ctx context.Context, tenantID, id, journalEntryID, actor string) error {
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetDisbursementCaseAndJournal(ctx, tenantID, item.ID, "", journalEntryID); err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetDisbursementStatus(ctx, tenantID, item.ID, domain.DisbursementPosted, actor); err != nil {
		return mapRepoError(err)
	}
	return s.repo.SettleRegisterDisbursement(ctx, tenantID, item.AgreementCode, item.DisburseAmtMinor)
}

// SettleComplete marks the COMPLETE leg POSTED and unwinds the in-transit
// pending on the agreement (guarded not below zero). The first completed
// drawdown statuses the contract ACTIVE.
func (s *DisbursementService) SettleComplete(ctx context.Context, tenantID, id, journalEntryID, actor string) error {
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetDisbursementCaseAndJournal(ctx, tenantID, item.ID, "", journalEntryID); err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetDisbursementStatus(ctx, tenantID, item.ID, domain.DisbursementPosted, actor); err != nil {
		return mapRepoError(err)
	}
	return s.repo.SettleCompleteDisbursement(ctx, tenantID, item.ContractCode, item.AgreementCode, item.DisburseAmtMinor)
}

func isValidISODate(v string) bool {
	if len(v) != 10 || v[4] != '-' || v[7] != '-' {
		return false
	}
	for _, i := range [2]int{0, 5} {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

// PostingDetail is everything the workflow worker needs to build the
// posting request (agreement + contract joined, flow-aware legs).
func (s *DisbursementService) PostingDetail(ctx context.Context, tenantID, id string) (*loanv1.DisbursementPostingDetail, error) {
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	agreement, err := s.repo.GetAgreementByCode(ctx, tenantID, item.AgreementCode)
	if err != nil {
		return nil, mapRepoError(err)
	}
	contract, err := s.repo.GetContractByCode(ctx, tenantID, item.ContractCode)
	if err != nil {
		return nil, mapRepoError(err)
	}
	flowType := item.FlowType
	if flowType == "" {
		flowType = domain.FlowRegister
	}
	detail := &loanv1.DisbursementPostingDetail{
		DisbursementId:   item.ID,
		DisbursementCode: item.ID,
		ContractCode:     item.ContractCode,
		AgreementCode:    item.AgreementCode,
		DisburseDate:     item.DisburseDate,
		DisburseAmtMinor: item.DisburseAmtMinor,
		CurrencyCode:     item.CurrencyCode,
		DebtGroupCode:    agreement.DebtGroupCode,
		OrgUnitCode:      contract.EmployeeCode,
		CustomerCode:     contract.CustomerCode,
		FundSourceCode:   item.FundSourceCode,
		FlowType:         flowType,
	}
	if item.WorkflowCaseID != nil {
		detail.WorkflowCaseId = *item.WorkflowCaseID
	}
	return detail, nil
}
