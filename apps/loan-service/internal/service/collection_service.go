package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
)

// CollectionService runs the LNM.301.02 receipt flow: create DRAFT →
// submit case (LNM_COLLECTION_V2) → workflow approval → the execute worker
// posts the 4-line LNM_COLLECTION rule card and applies side effects.
type CollectionService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
}

func NewCollectionService(repo *repository.LoanRepository, workflow AdjustmentSubmitter) *CollectionService {
	return &CollectionService{repo: repo, workflow: workflow}
}

// CaseType is the BPMN case type for the collection flow.
const CollectionCaseType = "LNM_COLLECTION_V2"

// List returns one page of the collection ledger. q matches the
// agreement/contract codes (ILIKE), sort/order are the whitelisted keys
// validated by the handler's ListSpec.
func (s *CollectionService) List(ctx context.Context, tenantID string, orgCodes []string, status, contractCode, q, sort, order string, page, perPage int) ([]domain.Collection, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	items, total, err := s.repo.ListCollections(ctx, tenantID, orgCodes, status, contractCode, q, sort, order, perPage, (page-1)*perPage)
	return items, total, mapRepoError(err)
}

func (s *CollectionService) Get(ctx context.Context, tenantID, id string) (domain.Collection, error) {
	item, err := s.repo.GetCollection(ctx, tenantID, id)
	if err != nil {
		return domain.Collection{}, mapRepoError(err)
	}
	return *item, nil
}

// Create registers a DRAFT collection. Principal or interest must be positive.
func (s *CollectionService) Create(ctx context.Context, tenantID, createdBy string, in *domain.Collection) (*domain.Collection, error) {
	if in == nil || strings.TrimSpace(in.ContractCode) == "" || strings.TrimSpace(in.AgreementCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "contract_code and agreement_code are required")
	}
	if in.PrincipalMinor <= 0 && in.InterestMinor <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "principal_minor or interest_minor must be positive")
	}
	if !isValidISODate(in.CollectionDate) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "collection_date must be YYYY-MM-DD")
	}
	if _, err := s.repo.GetAgreementByCode(ctx, tenantID, in.AgreementCode); err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "agreement_code not found: "+in.AgreementCode)
	}
	in.ID = repository.NewID("col")
	in.TenantID = tenantID
	in.Status = domain.CollectionDraft
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	in.CreatedBy = createdBy
	created, err := s.repo.CreateCollection(ctx, in)
	if err != nil {
		return nil, mapRepoError(err)
	}
	return created, nil
}

// Submit pushes the DRAFT collection into the LNM_COLLECTION_V2 case.
func (s *CollectionService) Submit(ctx context.Context, tenantID, actor, id string) (domain.Collection, error) {
	item, err := s.repo.GetCollection(ctx, tenantID, id)
	if err != nil {
		return domain.Collection{}, mapRepoError(err)
	}
	if item.Status != domain.CollectionDraft {
		return domain.Collection{}, ardaerrors.New(ardaerrors.CodeInvalidInput, "only DRAFT collections can be submitted")
	}
	if s.workflow == nil {
		return domain.Collection{}, ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          CollectionCaseType,
		CaseCode:          "",
		Title:             "Thu nợ — " + item.ContractCode + " / " + item.AgreementCode,
		PrimaryObjectType: "lnm.collection",
		PrimaryObjectID:   item.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("lnm-collection-%s", item.ID),
	})
	if err != nil {
		return domain.Collection{}, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	vars := map[string]any{
		"collectionId":  item.ID,
		"contractCode":  item.ContractCode,
		"agreementCode": item.AgreementCode,
		"collectionDate": item.CollectionDate,
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars, fmt.Sprintf("lnm-collection-%s-submit", item.ID)); err != nil {
		return domain.Collection{}, ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetCollectionCaseAndJournal(ctx, tenantID, item.ID, caseCreated.Id, ""); err != nil {
		return domain.Collection{}, mapRepoError(err)
	}
	if err := s.repo.SetCollectionStatus(ctx, tenantID, item.ID, domain.CollectionSubmitted, actor); err != nil {
		return domain.Collection{}, mapRepoError(err)
	}
	updated, err := s.repo.GetCollection(ctx, tenantID, item.ID)
	if err != nil {
		return domain.Collection{}, mapRepoError(err)
	}
	return *updated, nil
}

// Check validates the collection is actionable (BPMN validate job).
func (s *CollectionService) Check(ctx context.Context, tenantID, id string) (bool, string, error) {
	item, err := s.repo.GetCollection(ctx, tenantID, id)
	if err != nil {
		return false, mapRepoError(err).Error(), nil
	}
	if item.Status != domain.CollectionSubmitted {
		return false, fmt.Sprintf("status %s is not actionable", item.Status), nil
	}
	return true, "", nil
}

// Resolve applies the workflow decision without posting.
func (s *CollectionService) Resolve(ctx context.Context, tenantID, id, decision, decidedBy, note string) error {
	status := domain.CollectionRejected
	if decision == "APPROVE" {
		status = domain.CollectionApproved
	}
	if err := s.repo.SetCollectionStatus(ctx, tenantID, id, status, decidedBy); err != nil {
		return mapRepoError(err)
	}
	return nil
}

// Settle marks POSTED with the journal entry and applies side effects —
// executed by the workflow worker after a successful PostTransaction.
func (s *CollectionService) Settle(ctx context.Context, tenantID, id, journalEntryID, actor string) error {
	if err := s.repo.SetCollectionCaseAndJournal(ctx, tenantID, id, "", journalEntryID); err != nil {
		return mapRepoError(err)
	}
	item, err := s.repo.GetCollection(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetCollectionStatus(ctx, tenantID, id, domain.CollectionPosted, actor); err != nil {
		return mapRepoError(err)
	}
	return s.repo.ApplyCollection(ctx, tenantID, item.AgreementCode, item.PrincipalMinor, item.InterestMinor)
}

// PostingDetail is everything the workflow worker needs for the 4-line
// LNM_COLLECTION posting (agreement + contract joined).
func (s *CollectionService) PostingDetail(ctx context.Context, tenantID, id string) (*loanv1.CollectionPostingDetail, error) {
	item, err := s.repo.GetCollection(ctx, tenantID, id)
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
	detail := &loanv1.CollectionPostingDetail{
		CollectionId:    item.ID,
		ContractCode:    item.ContractCode,
		AgreementCode:   item.AgreementCode,
		CollectionDate:  item.CollectionDate,
		PrincipalMinor:  item.PrincipalMinor,
		InterestMinor:   item.InterestMinor,
		CurrencyCode:    item.CurrencyCode,
		DebtGroupCode:   agreement.DebtGroupCode,
		OrgUnitCode:     contract.EmployeeCode,
		CustomerCode:    contract.CustomerCode,
	}
	if item.WorkflowCaseID != nil {
		detail.WorkflowCaseId = *item.WorkflowCaseID
	}
	return detail, nil
}
