package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// DisbursementService runs the LNM.300.02 drawdown flow: create DRAFT →
// submit case (LNM_DISBURSEMENT_V1) → workflow approval → posting step
// executes LNM_DISBURSEMENT via the finance PostingService (worker side).
type DisbursementService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
}

func NewDisbursementService(repo *repository.LoanRepository, workflow AdjustmentSubmitter) *DisbursementService {
	return &DisbursementService{repo: repo, workflow: workflow}
}

// CaseType is the BPMN case type for the disbursement flow.
const CaseType = "LNM_DISBURSEMENT_V1"

func (s *DisbursementService) List(ctx context.Context, tenantID, status, contractCode string) ([]domain.Disbursement, error) {
	items, err := s.repo.ListDisbursements(ctx, tenantID, status, contractCode)
	return items, mapRepoError(err)
}

func (s *DisbursementService) Get(ctx context.Context, tenantID, id string) (domain.Disbursement, error) {
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return domain.Disbursement{}, mapRepoError(err)
	}
	return *item, nil
}

// Create registers a DRAFT disbursement. The agreement must exist; the
// amount must be positive (int64 minor units).
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
	if _, err := s.repo.GetAgreementByCode(ctx, tenantID, in.AgreementCode); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "agreement_code not found: "+in.AgreementCode)
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

// Submit pushes the DRAFT disbursement into the LNM_DISBURSEMENT_V1 case.
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
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CaseType,
		CaseCode:          "",
		Title:             "Giải ngân — " + item.ContractCode + " / " + item.AgreementCode,
		PrimaryObjectType: "lnm.disbursement",
		PrimaryObjectID:   item.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("lnm-disbursement-%s", item.ID),
	})
	if err != nil {
		return domain.Disbursement{}, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	vars := map[string]any{
		"disbursementId": item.ID,
		"contractCode":   item.ContractCode,
		"agreementCode":  item.AgreementCode,
		"disburseDate":   item.DisburseDate,
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars, fmt.Sprintf("lnm-disbursement-%s-submit", item.ID)); err != nil {
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

// Resolve applies the workflow decision. APPROVED + posting success marks
// POSTED and settles the agreement outstanding; REJECTED/CANCELLED are
// terminal without posting.
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

// Settle marks POSTED with the journal entry id and updates agreement
// outstanding — executed by the workflow worker right after a successful
// PostTransaction.
func (s *DisbursementService) Settle(ctx context.Context, tenantID, id, journalEntryID, actor string) error {
	if err := s.repo.SetDisbursementCaseAndJournal(ctx, tenantID, id, "", journalEntryID); err != nil {
		return mapRepoError(err)
	}
	item, err := s.repo.GetDisbursement(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetDisbursementStatus(ctx, tenantID, id, domain.DisbursementPosted, actor); err != nil {
		return mapRepoError(err)
	}
	return s.repo.SettleDisbursement(ctx, tenantID, item.AgreementCode, item.DisburseAmtMinor)
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
