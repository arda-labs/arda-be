package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	workflowclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/workflow"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
)

// BatchCollectionRowInput is one receipt row of a batch collection request.
type BatchCollectionRowInput struct {
	ContractCode         string `json:"contract_code"`
	AgreementCode        string `json:"agreement_code"`
	PrincipalMinor       int64  `json:"principal_minor"`
	InterestMinor        int64  `json:"interest_minor"`
	OverdueInterestMinor int64  `json:"overdue_interest_minor"`
}

// CreateBatchInputCollection is the batch collection request body.
type CreateBatchInputCollection struct {
	OrgCode       string                     `json:"org_code,omitempty"`
	TxnDate       string                     `json:"txn_date"`
	PaymentMethod string                     `json:"payment_method,omitempty"`
	AccountCode   string                     `json:"account_code,omitempty"`
	Description   string                     `json:"description,omitempty"`
	Trader        map[string]string          `json:"trader,omitempty"`
	Rows          []BatchCollectionRowInput  `json:"rows"`
}

// batchCollectionRowVars is the camelCase row shape stamped into the case
// variables.
type batchCollectionRowVars struct {
	ContractCode         string `json:"contractCode"`
	AgreementCode        string `json:"agreementCode"`
	PrincipalMinor       int64  `json:"principalMinor"`
	InterestMinor        int64  `json:"interestMinor"`
	OverdueInterestMinor int64  `json:"overdueInterestMinor,omitempty"`
	RowID                string `json:"rowId,omitempty"`
}

// BatchCollectionService runs the batch (1 hồ sơ thu nợ — N hợp đồng)
// receipt flow: one LNM_COLLECTION_BATCH_V2 case, its rows are the
// per-agreement lnm_collections legs settled by the same side effects as
// the single-row path. Posting rides the finance two-phase lifecycle
// worker-side (cash DEBIT legs + principal/interest CREDIT pairs per row).
type BatchCollectionService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
}

func NewBatchCollectionService(repo *repository.LoanRepository, workflow AdjustmentSubmitter) *BatchCollectionService {
	return &BatchCollectionService{repo: repo, workflow: workflow}
}

// CreateBatchCollection creates a DRAFT collection batch + rows in one tx,
// then opens the LNM_COLLECTION_BATCH_V2 case (SUBMITTED). Guard per row:
// the agreement exists and the principal may not exceed its outstanding.
func (s *BatchCollectionService) CreateBatchCollection(ctx context.Context, tenantID, actor, orgCode string, in *CreateBatchInputCollection) (*domain.CollectionBatch, error) {
	if in == nil || len(in.Rows) == 0 {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "rows must not be empty")
	}
	if !isValidISODate(in.TxnDate) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "txn_date must be YYYY-MM-DD")
	}
	paymentMethod := strings.ToUpper(strings.TrimSpace(in.PaymentMethod))
	if paymentMethod == "" {
		paymentMethod = "TRANSFER"
	}
	if paymentMethod != "CASH" && paymentMethod != "TRANSFER" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "payment_method must be one of: CASH, TRANSFER")
	}
	batch := &domain.CollectionBatch{
		ID:            repository.NewID("colbatch"),
		TenantID:      tenantID,
		OrgCode:       orgCode,
		TxnDate:       in.TxnDate,
		PaymentMethod: paymentMethod,
		AccountCode:   in.AccountCode,
		CurrencyCode:  "VND",
		Description:   in.Description,
		Trader:        in.Trader,
		Status:        domain.BatchSubmitted,
		CreatedBy:     actor,
		Rows:          make([]domain.Collection, 0, len(in.Rows)),
	}
	if batch.Trader == nil {
		batch.Trader = map[string]string{}
	}
	totalPrincipal, totalInterest := int64(0), int64(0)
	for i := range in.Rows {
		row := &in.Rows[i]
		row.ContractCode = strings.TrimSpace(row.ContractCode)
		row.AgreementCode = strings.TrimSpace(row.AgreementCode)
		if row.ContractCode == "" || row.AgreementCode == "" {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: contract_code and agreement_code are required", i))
		}
		if row.PrincipalMinor < 0 || row.InterestMinor < 0 || row.OverdueInterestMinor < 0 {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: amounts must not be negative", i))
		}
		if row.PrincipalMinor+row.InterestMinor+row.OverdueInterestMinor <= 0 {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: principal_minor or interest_minor must be positive", i))
		}
		agreement, err := s.repo.GetAgreementByCode(ctx, tenantID, row.AgreementCode)
		if err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: agreement_code not found: %s", i, row.AgreementCode))
		}
		if agreement.ContractCode != row.ContractCode {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: agreement %s does not belong to contract %s", i, row.AgreementCode, row.ContractCode))
		}
		if row.PrincipalMinor > agreement.OutstandingAmt {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf(
				"rows[%d]: principal_minor %d exceeds agreement %s outstanding %d",
				i, row.PrincipalMinor, row.AgreementCode, agreement.OutstandingAmt))
		}
		totalPrincipal += row.PrincipalMinor
		totalInterest += row.InterestMinor + row.OverdueInterestMinor
		batch.Rows = append(batch.Rows, domain.Collection{
			TenantID:             tenantID,
			ContractCode:         row.ContractCode,
			AgreementCode:        row.AgreementCode,
			CollectionDate:       batch.TxnDate,
			PrincipalMinor:       row.PrincipalMinor,
			InterestMinor:        row.InterestMinor,
			OverdueInterestMinor: row.OverdueInterestMinor,
			CurrencyCode:         batch.CurrencyCode,
			BatchID:              batch.ID,
			OrgCode:              orgCode,
			CreatedBy:            actor,
			Status:               domain.CollectionDraft,
		})
	}
	batch.TotalPrincipalMinor = totalPrincipal
	batch.TotalInterestMinor = totalInterest
	if err := s.repo.CreateCollectionBatch(ctx, batch); err != nil {
		return nil, mapRepoError(err)
	}
	if err := s.submitBatchCase(ctx, tenantID, actor, batch, batchCollectionRowVarsList(batch.Rows, in.Rows)); err != nil {
		return nil, err
	}
	return batch, nil
}

func batchCollectionRowVarsList(rows []domain.Collection, inputs []BatchCollectionRowInput) []batchCollectionRowVars {
	out := make([]batchCollectionRowVars, 0, len(rows))
	for i := range rows {
		v := batchCollectionRowVars{
			ContractCode:         rows[i].ContractCode,
			AgreementCode:        rows[i].AgreementCode,
			PrincipalMinor:       rows[i].PrincipalMinor,
			InterestMinor:        rows[i].InterestMinor,
			OverdueInterestMinor: rows[i].OverdueInterestMinor,
			RowID:                rows[i].ID,
		}
		_ = inputs
		out = append(out, v)
	}
	return out
}

// submitBatchCase opens + submits the collection batch's workflow case,
// stamps the case on the header and flips it SUBMITTED.
func (s *BatchCollectionService) submitBatchCase(ctx context.Context, tenantID, actor string, batch *domain.CollectionBatch, rows []batchCollectionRowVars) error {
	if s.workflow == nil {
		return ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          BatchCollectionCaseType,
		CaseCode:          "",
		Title:             "Thu nợ theo hồ sơ — " + batch.ID + " (" + batch.TxnDate + ")",
		PrimaryObjectType: "lnm.collection_batch",
		PrimaryObjectID:   batch.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("lnm-collection-batch-%s", batch.ID),
	})
	if err != nil {
		return ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	vars := map[string]any{
		"batchId":               batch.ID,
		"batchType":             domain.BatchTypeCollection,
		"postingIdempotencyKey": fmt.Sprintf("lnm-collection-batch-%s", batch.ID),
		"collectionBatch":       collectionBatchVars(batch, rows),
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars, fmt.Sprintf("lnm-collection-batch-%s-submit", batch.ID)); err != nil {
		return ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetCollectionBatchCase(ctx, tenantID, batch.ID, caseCreated.Id, caseCreated.GetCaseCode()); err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetCollectionBatchStatus(ctx, tenantID, batch.ID, domain.BatchSubmitted); err != nil {
		return mapRepoError(err)
	}
	batch.Status = domain.BatchSubmitted
	caseID := caseCreated.Id
	caseCode := caseCreated.GetCaseCode()
	batch.WorkflowCaseID = &caseID
	batch.WorkflowCaseCode = caseCode
	return nil
}

// collectionBatchVars is the camelCase batch block carried in the case
// variables (input snapshot for the maker step).
func collectionBatchVars(batch *domain.CollectionBatch, rows []batchCollectionRowVars) map[string]any {
	return map[string]any{
		"batchId":             batch.ID,
		"txnDate":             batch.TxnDate,
		"paymentMethod":       batch.PaymentMethod,
		"accountCode":         batch.AccountCode,
		"currencyCode":        batch.CurrencyCode,
		"totalPrincipalMinor": batch.TotalPrincipalMinor,
		"totalInterestMinor":  batch.TotalInterestMinor,
		"description":         batch.Description,
		"trader":              batch.Trader,
		"rows":                rows,
	}
}

// List returns one page of the collection batch ledger.
func (s *BatchCollectionService) List(ctx context.Context, tenantID, status string, page, perPage int) ([]domain.CollectionBatch, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	items, total, err := s.repo.ListCollectionBatches(ctx, tenantID, status, perPage, (page-1)*perPage)
	return items, total, mapRepoError(err)
}

// Get returns one collection batch header with its rows.
func (s *BatchCollectionService) Get(ctx context.Context, tenantID, id string) (*domain.CollectionBatch, error) {
	batch, err := s.repo.GetCollectionBatch(ctx, tenantID, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	rows, err := s.repo.GetCollectionBatchRows(ctx, tenantID, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	batch.Rows = rows
	return batch, nil
}

// Check validates the batch is actionable (BPMN validate job): SUBMITTED and
// every row's principal still fits the agreement outstanding.
func (s *BatchCollectionService) Check(ctx context.Context, tenantID, id string) (bool, string, error) {
	batch, err := s.repo.GetCollectionBatch(ctx, tenantID, id)
	if err != nil {
		return false, mapRepoError(err).Error(), nil
	}
	if batch.Status != domain.BatchSubmitted {
		return false, fmt.Sprintf("status %s is not actionable", batch.Status), nil
	}
	rows, err := s.repo.GetCollectionBatchRows(ctx, tenantID, id)
	if err != nil {
		return false, mapRepoError(err).Error(), nil
	}
	for i, row := range rows {
		agreement, err := s.repo.GetAgreementByCode(ctx, tenantID, row.AgreementCode)
		if err != nil {
			return false, fmt.Sprintf("rows[%d]: agreement %s not found", i, row.AgreementCode), nil
		}
		if row.PrincipalMinor > agreement.OutstandingAmt {
			return false, fmt.Sprintf("rows[%d]: principal exceeds agreement %s outstanding", i, row.AgreementCode), nil
		}
	}
	return true, "", nil
}

// Resolve applies the workflow decision without posting.
func (s *BatchCollectionService) Resolve(ctx context.Context, tenantID, id, decision string) error {
	status := domain.BatchRejected
	if decision == "APPROVE" {
		status = domain.BatchApproved
	}
	if err := s.repo.SetCollectionBatchStatus(ctx, tenantID, id, status); err != nil {
		return mapRepoError(err)
	}
	return nil
}

// BatchPostingDetail is everything the workflow worker needs to build the
// batch collection posting request: header + per-row agreement/contract
// context (overdue interest rides the interest legs, EPAS 301).
func (s *BatchCollectionService) BatchPostingDetail(ctx context.Context, tenantID, batchID string) (*loanv1.BatchPostingDetail, error) {
	batch, err := s.repo.GetCollectionBatch(ctx, tenantID, batchID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	rows, err := s.repo.GetCollectionBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	detail := &loanv1.BatchPostingDetail{
		BatchId:             batch.ID,
		BatchCode:           batch.ID,
		BatchType:           domain.BatchTypeCollection,
		TxnDate:             batch.TxnDate,
		PaymentMethod:       batch.PaymentMethod,
		AccountCode:         batch.AccountCode,
		CurrencyCode:        batch.CurrencyCode,
		TotalAmtMinor:       batch.TotalPrincipalMinor + batch.TotalInterestMinor,
		TotalPrincipalMinor: batch.TotalPrincipalMinor,
		TotalInterestMinor:  batch.TotalInterestMinor,
		Description:         batch.Description,
		OrgUnitCode:         batch.OrgCode,
		Trader:              batch.Trader,
	}
	if batch.WorkflowCaseID != nil {
		detail.WorkflowCaseId = *batch.WorkflowCaseID
	}
	for _, row := range rows {
		agreement, err := s.repo.GetAgreementByCode(ctx, tenantID, row.AgreementCode)
		if err != nil {
			return nil, mapRepoError(err)
		}
		contract, err := s.repo.GetContractByCode(ctx, tenantID, row.ContractCode)
		if err != nil {
			return nil, mapRepoError(err)
		}
		detail.Rows = append(detail.Rows, &loanv1.BatchRowDetail{
			RowId:                row.ID,
			ContractCode:         row.ContractCode,
			AgreementCode:        row.AgreementCode,
			PlanCode:             agreement.PlanCode,
			PrincipalMinor:       row.PrincipalMinor,
			InterestMinor:        row.InterestMinor,
			OverdueInterestMinor: row.OverdueInterestMinor,
			DebtGroupCode:        agreement.DebtGroupCode,
			OrgUnitCode:          contract.EmployeeCode,
			CustomerCode:         contract.CustomerCode,
		})
	}
	return detail, nil
}

// SettleBatchCollection marks the batch + its rows POSTED and applies the
// per-row receipt side effects (outstanding unwind, collected counters,
// overdue interest included) — the exact per-row ApplyCollection semantics,
// looped.
func (s *BatchCollectionService) SettleBatchCollection(ctx context.Context, tenantID, batchID, journalEntryID, actor string) error {
	rows, err := s.repo.GetCollectionBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return mapRepoError(err)
	}
	for _, row := range rows {
		if err := s.repo.SetCollectionCaseAndJournal(ctx, tenantID, row.ID, "", "", journalEntryID); err != nil {
			return mapRepoError(err)
		}
		if err := s.repo.SetCollectionStatus(ctx, tenantID, row.ID, domain.CollectionPosted, actor); err != nil {
			return mapRepoError(err)
		}
		if err := s.repo.ApplyCollection(ctx, tenantID, row.AgreementCode, row.PrincipalMinor, row.InterestMinor+row.OverdueInterestMinor); err != nil {
			return mapRepoError(err)
		}
	}
	return s.repo.SetCollectionBatchPosted(ctx, tenantID, batchID, journalEntryID)
}
