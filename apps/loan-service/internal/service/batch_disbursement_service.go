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

// Batch case types (iteration 13): the batch dossier rides its own v2 case
// type — the per-row single-disbursement case types stay untouched.
const (
	BatchDisbRegisterCaseType = "LNM_DISB_BATCH_REGISTER_V2"
	BatchDisbCompleteCaseType = "LNM_DISB_BATCH_COMPLETE_V2"
	BatchCollectionCaseType   = "LNM_COLLECTION_BATCH_V2"
)

// BatchRowInput is one drawdown row of a batch register request.
type BatchRowInput struct {
	ContractCode  string `json:"contract_code"`
	AgreementCode string `json:"agreement_code"`
	// PlanCode is optional input metadata; the authoritative value comes
	// from the agreement (lnm_agreements.plan_code snapshot on the row's
	// contract context — the agreement carries the group key).
	PlanCode   string `json:"plan_code,omitempty"`
	AmountMinor int64  `json:"amount_minor"`
	// IsClosed marks a closing row: no cash moves (amount 0), the contract
	// is CLOSED after settle. Only valid on the COMPLETE batch.
	IsClosed bool `json:"is_closed,omitempty"`
}

// CreateBatchInput is the batch register/complete request body.
type CreateBatchInput struct {
	OrgCode       string            `json:"org_code,omitempty"`
	TxnDate       string            `json:"txn_date"`
	PaymentMethod string            `json:"payment_method,omitempty"`
	AccountCode   string            `json:"account_code,omitempty"`
	Description   string            `json:"description,omitempty"`
	Trader        map[string]string `json:"trader,omitempty"`
	Rows          []BatchRowInput   `json:"rows"`
}

// BatchDisbursementService runs the batch (1 hồ sơ — N hợp đồng) two-phase
// drawdown: one batch dossier per case, its rows are the per-agreement
// lnm_disbursements legs settled by the same side effects as the single-row
// path. Posting rides the finance two-phase lifecycle worker-side (1
// PostingRequest, N DEBIT/CREDIT line pairs).
type BatchDisbursementService struct {
	repo     *repository.LoanRepository
	workflow AdjustmentSubmitter
}

func NewBatchDisbursementService(repo *repository.LoanRepository, workflow AdjustmentSubmitter) *BatchDisbursementService {
	return &BatchDisbursementService{repo: repo, workflow: workflow}
}

// batchRowVars is the camelCase row shape stamped into the case variables
// (mirrors the batch input the FE submitted).
type batchRowVars struct {
	ContractCode  string `json:"contractCode"`
	AgreementCode string `json:"agreementCode"`
	PlanCode      string `json:"planCode,omitempty"`
	AmountMinor   int64  `json:"amountMinor"`
	IsClosed      bool   `json:"isClosed,omitempty"`
	RowID         string `json:"rowId,omitempty"`
}

// CreateBatchRegister creates a DRAFT batch REGISTER dossier and its rows in
// one tx, then opens the LNM_DISB_BATCH_REGISTER_V2 case (SUBMITTED). Guard
// per row: agreement exists + belongs to the contract, amount > 0, and the
// contract headroom (loan amount − Σoutstanding − Σpending) still covers the
// amount with the batch's earlier rows already deducted (running sum).
func (s *BatchDisbursementService) CreateBatchRegister(ctx context.Context, tenantID, actor, orgCode string, in *CreateBatchInput) (*domain.DisbursementBatch, error) {
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
	batch := &domain.DisbursementBatch{
		ID:            repository.NewID("disbbatch"),
		TenantID:      tenantID,
		OrgCode:       orgCode,
		FlowType:      domain.FlowRegister,
		TxnDate:       in.TxnDate,
		PaymentMethod: paymentMethod,
		AccountCode:   in.AccountCode,
		CurrencyCode:  "VND",
		Description:   in.Description,
		Trader:        in.Trader,
		Status:        domain.BatchSubmitted,
		CreatedBy:     actor,
		Rows:          make([]domain.Disbursement, 0, len(in.Rows)),
	}
	if batch.Trader == nil {
		batch.Trader = map[string]string{}
	}
	// Headroom is per contract: the running sum tracks this batch's own
	// earlier rows so N rows of the same contract cannot jointly overshoot.
	runningUsed := map[string]int64{}
	total := int64(0)
	for i := range in.Rows {
		row := &in.Rows[i]
		row.ContractCode = strings.TrimSpace(row.ContractCode)
		row.AgreementCode = strings.TrimSpace(row.AgreementCode)
		if row.ContractCode == "" || row.AgreementCode == "" {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: contract_code and agreement_code are required", i))
		}
		if row.AmountMinor <= 0 {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: amount_minor must be positive", i))
		}
		agreement, err := s.repo.GetAgreementByCode(ctx, tenantID, row.AgreementCode)
		if err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: agreement_code not found: %s", i, row.AgreementCode))
		}
		if agreement.ContractCode != row.ContractCode {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: agreement %s does not belong to contract %s", i, row.AgreementCode, row.ContractCode))
		}
		contract, err := s.repo.GetContractByCode(ctx, tenantID, row.ContractCode)
		if err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: contract_code not found: %s", i, row.ContractCode))
		}
		outstanding, pending, err := s.repo.SumOutstandingAndPendingByContract(ctx, tenantID, row.ContractCode)
		if err != nil {
			return nil, mapRepoError(err)
		}
		headroom := contract.LoanAmt - outstanding - pending - runningUsed[row.ContractCode]
		if row.AmountMinor > headroom {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf(
				"rows[%d]: amount_minor %d exceeds contract %s headroom %d (loan_amt_minor %d - outstanding %d - pending %d - batch rows %d)",
				i, row.AmountMinor, row.ContractCode, headroom, contract.LoanAmt, outstanding, pending, runningUsed[row.ContractCode]))
		}
		runningUsed[row.ContractCode] += row.AmountMinor
		total += row.AmountMinor
		batch.Rows = append(batch.Rows, domain.Disbursement{
			TenantID:         tenantID,
			ContractCode:     row.ContractCode,
			AgreementCode:    row.AgreementCode,
			DisburseDate:     batch.TxnDate,
			DisburseAmtMinor: row.AmountMinor,
			CurrencyCode:     batch.CurrencyCode,
			FlowType:         domain.FlowRegister,
			BatchID:          batch.ID,
			OrgCode:          orgCode,
			CreatedBy:        actor,
			Status:           domain.DisbursementDraft,
		})
	}
	batch.TotalAmtMinor = total
	if err := s.repo.CreateDisbursementBatch(ctx, batch); err != nil {
		return nil, mapRepoError(err)
	}
	if err := s.submitBatchCase(ctx, tenantID, actor, batch, BatchDisbRegisterCaseType, "Đăng ký giải ngân theo hồ sơ", "lnm-disb-batch-register", batchRowVarsList(batch.Rows, in.Rows)); err != nil {
		return nil, err
	}
	return batch, nil
}

// CreateBatchComplete creates a COMPLETE batch against a POSTED REGISTER
// batch. Per row the amount may not exceed the original register row's
// un-completed remainder (Σ COMPLETE booked on that agreement so far counts,
// in-flight included). is_closed rows carry amount 0 and close the contract
// after settle.
func (s *BatchDisbursementService) CreateBatchComplete(ctx context.Context, tenantID, actor, orgCode, sourceBatchID string, in *CreateBatchInput) (*domain.DisbursementBatch, error) {
	if strings.TrimSpace(sourceBatchID) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "source_batch_id is required for COMPLETE batches")
	}
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
	source, err := s.repo.GetDisbursementBatch(ctx, tenantID, sourceBatchID)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "source_batch_id not found: "+sourceBatchID)
	}
	if source.FlowType != domain.FlowRegister {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "source_batch_id must reference a REGISTER batch")
	}
	if source.Status != domain.BatchPosted {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "source register batch is not POSTED yet")
	}
	sourceRows, err := s.repo.GetBatchRows(ctx, tenantID, source.ID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	sourceByAgreement := make(map[string]domain.Disbursement, len(sourceRows))
	for _, sr := range sourceRows {
		sourceByAgreement[sr.AgreementCode] = sr
	}
	batch := &domain.DisbursementBatch{
		ID:            repository.NewID("disbbatch"),
		TenantID:      tenantID,
		OrgCode:       orgCode,
		FlowType:      domain.FlowComplete,
		SourceBatchID: source.ID,
		TxnDate:       in.TxnDate,
		PaymentMethod: paymentMethod,
		AccountCode:   in.AccountCode,
		CurrencyCode:  source.CurrencyCode,
		Description:   in.Description,
		Trader:        in.Trader,
		Status:        domain.BatchSubmitted,
		CreatedBy:     actor,
		Rows:          make([]domain.Disbursement, 0, len(in.Rows)),
	}
	if batch.Trader == nil {
		batch.Trader = map[string]string{}
	}
	total := int64(0)
	for i := range in.Rows {
		row := &in.Rows[i]
		row.ContractCode = strings.TrimSpace(row.ContractCode)
		row.AgreementCode = strings.TrimSpace(row.AgreementCode)
		sourceRow, ok := sourceByAgreement[row.AgreementCode]
		if !ok {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: agreement %s is not part of the source register batch", i, row.AgreementCode))
		}
		if row.ContractCode == "" {
			row.ContractCode = sourceRow.ContractCode
		}
		if row.ContractCode != sourceRow.ContractCode {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: contract_code does not match the source register row", i))
		}
		if row.IsClosed {
			row.AmountMinor = 0
		}
		if row.AmountMinor < 0 {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: amount_minor must not be negative", i))
		}
		completed, err := s.repo.SumCompleteForAgreement(ctx, tenantID, row.AgreementCode)
		if err != nil {
			return nil, mapRepoError(err)
		}
		remainder := sourceRow.DisburseAmtMinor - completed
		if row.AmountMinor > remainder {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf(
				"rows[%d]: amount_minor %d exceeds source remainder %d for agreement %s (register %d - completed %d)",
				i, row.AmountMinor, remainder, row.AgreementCode, sourceRow.DisburseAmtMinor, completed))
		}
		total += row.AmountMinor
		batch.Rows = append(batch.Rows, domain.Disbursement{
			TenantID:         tenantID,
			ContractCode:     row.ContractCode,
			AgreementCode:    row.AgreementCode,
			DisburseDate:     batch.TxnDate,
			DisburseAmtMinor: row.AmountMinor,
			CurrencyCode:     batch.CurrencyCode,
			FlowType:         domain.FlowComplete,
			SourceRegisterID: sourceRow.ID,
			BatchID:          batch.ID,
			IsClosed:         row.IsClosed,
			OrgCode:          orgCode,
			CreatedBy:        actor,
			Status:           domain.DisbursementDraft,
		})
	}
	batch.TotalAmtMinor = total
	if err := s.repo.CreateDisbursementBatch(ctx, batch); err != nil {
		return nil, mapRepoError(err)
	}
	if err := s.submitBatchCase(ctx, tenantID, actor, batch, BatchDisbCompleteCaseType, "Hoàn tất giải ngân theo hồ sơ", "lnm-disb-batch-complete", batchRowVarsList(batch.Rows, in.Rows)); err != nil {
		return nil, err
	}
	return batch, nil
}

// batchRowVarsList merges the stored rows (row IDs) with the request inputs
// (plan metadata) into the camelCase case-variable shape.
func batchRowVarsList(rows []domain.Disbursement, inputs []BatchRowInput) []batchRowVars {
	out := make([]batchRowVars, 0, len(rows))
	for i := range rows {
		v := batchRowVars{
			ContractCode:  rows[i].ContractCode,
			AgreementCode: rows[i].AgreementCode,
			AmountMinor:   rows[i].DisburseAmtMinor,
			IsClosed:      rows[i].IsClosed,
			RowID:         rows[i].ID,
		}
		if i < len(inputs) {
			v.PlanCode = inputs[i].PlanCode
		}
		out = append(out, v)
	}
	return out
}

// submitBatchCase opens + submits the batch's workflow case, stamps the case
// on the batch header and flips it SUBMITTED.
func (s *BatchDisbursementService) submitBatchCase(ctx context.Context, tenantID, actor string, batch *domain.DisbursementBatch, caseType, titlePrefix, idempotencyPrefix string, rows []batchRowVars) error {
	if s.workflow == nil {
		return ardaerrors.New(ardaerrors.CodeInternal, "workflow client is not configured")
	}
	caseCreated, err := s.workflow.CreateCase(ctx, workflowclient.CaseCreate{
		TenantID:          tenantID,
		CaseType:          caseType,
		CaseCode:          "",
		Title:             titlePrefix + " — " + batch.ID + " (" + batch.TxnDate + ")",
		PrimaryObjectType: "lnm.disbursement_batch",
		PrimaryObjectID:   batch.ID,
		DomainService:     "loan-service",
		Priority:          "NORMAL",
		CreatedBy:         actor,
		IdempotencyKey:    fmt.Sprintf("%s-%s", idempotencyPrefix, batch.ID),
	})
	if err != nil {
		return ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow create case failed", err)
	}
	vars := map[string]any{
		"batchId":                batch.ID,
		"batchType":              batchTypeOf(caseType),
		"postingIdempotencyKey":  fmt.Sprintf("lnm-disb-batch-%s", batch.ID),
		"disbursementBatch":      disbursementBatchVars(batch, rows),
	}
	if _, err = s.workflow.SubmitCase(ctx, caseCreated.Id, actor, vars, fmt.Sprintf("%s-%s-submit", idempotencyPrefix, batch.ID)); err != nil {
		return ardaerrors.Wrap(ardaerrors.CodeBadGateway, "workflow submit case failed", err)
	}
	if err := s.repo.SetDisbursementBatchCase(ctx, tenantID, batch.ID, caseCreated.Id, caseCreated.GetCaseCode()); err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetDisbursementBatchStatus(ctx, tenantID, batch.ID, domain.BatchSubmitted); err != nil {
		return mapRepoError(err)
	}
	batch.Status = domain.BatchSubmitted
	caseID := caseCreated.Id
	caseCode := caseCreated.GetCaseCode()
	batch.WorkflowCaseID = &caseID
	batch.WorkflowCaseCode = caseCode
	return nil
}

func batchTypeOf(caseType string) string {
	if caseType == BatchDisbCompleteCaseType {
		return domain.BatchTypeDisbComplete
	}
	if caseType == BatchCollectionCaseType {
		return domain.BatchTypeCollection
	}
	return domain.BatchTypeDisbRegister
}

// disbursementBatchVars is the camelCase batch block carried in the case
// variables (input snapshot for the maker step).
func disbursementBatchVars(batch *domain.DisbursementBatch, rows []batchRowVars) map[string]any {
	return map[string]any{
		"batchId":       batch.ID,
		"flowType":      batch.FlowType,
		"txnDate":       batch.TxnDate,
		"paymentMethod": batch.PaymentMethod,
		"accountCode":   batch.AccountCode,
		"currencyCode":  batch.CurrencyCode,
		"totalAmtMinor": batch.TotalAmtMinor,
		"description":   batch.Description,
		"trader":        batch.Trader,
		"rows":          rows,
	}
}

// List returns one page of the disbursement batch ledger.
func (s *BatchDisbursementService) List(ctx context.Context, tenantID, status, flowType string, page, perPage int) ([]domain.DisbursementBatch, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	items, total, err := s.repo.ListDisbursementBatches(ctx, tenantID, status, flowType, perPage, (page-1)*perPage)
	return items, total, mapRepoError(err)
}

// Get returns one batch header with its rows.
func (s *BatchDisbursementService) Get(ctx context.Context, tenantID, id string) (*domain.DisbursementBatch, error) {
	batch, err := s.repo.GetDisbursementBatch(ctx, tenantID, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	rows, err := s.repo.GetBatchRows(ctx, tenantID, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	batch.Rows = rows
	return batch, nil
}

// Check validates the batch is actionable (BPMN validate job): SUBMITTED and
// every row still fits its guard (headroom for REGISTER rows, remainder for
// COMPLETE rows) — re-checked because the maker may have edited the case.
func (s *BatchDisbursementService) Check(ctx context.Context, tenantID, id string) (bool, string, error) {
	batch, err := s.repo.GetDisbursementBatch(ctx, tenantID, id)
	if err != nil {
		return false, mapRepoError(err).Error(), nil
	}
	if batch.Status != domain.BatchSubmitted {
		return false, fmt.Sprintf("status %s is not actionable", batch.Status), nil
	}
	rows, err := s.repo.GetBatchRows(ctx, tenantID, id)
	if err != nil {
		return false, mapRepoError(err).Error(), nil
	}
	runningUsed := map[string]int64{}
	for i, row := range rows {
		if row.FlowType == domain.FlowComplete {
			if row.IsClosed || row.DisburseAmtMinor == 0 {
				continue
			}
			completed, err := s.repo.SumCompleteForAgreement(ctx, tenantID, row.AgreementCode)
			if err != nil {
				return false, mapRepoError(err).Error(), nil
			}
			sourceRow, err := s.repo.GetDisbursement(ctx, tenantID, row.SourceRegisterID)
			if err != nil {
				return false, fmt.Sprintf("rows[%d]: source register row not found", i), nil
			}
			if row.DisburseAmtMinor > sourceRow.DisburseAmtMinor-completed {
				return false, fmt.Sprintf("rows[%d]: amount exceeds source remainder for agreement %s", i, row.AgreementCode), nil
			}
			continue
		}
		contract, err := s.repo.GetContractByCode(ctx, tenantID, row.ContractCode)
		if err != nil {
			return false, fmt.Sprintf("rows[%d]: contract %s not found", i, row.ContractCode), nil
		}
		outstanding, pending, err := s.repo.SumOutstandingAndPendingByContract(ctx, tenantID, row.ContractCode)
		if err != nil {
			return false, mapRepoError(err).Error(), nil
		}
		headroom := contract.LoanAmt - outstanding - pending - runningUsed[row.ContractCode]
		if row.DisburseAmtMinor > headroom {
			return false, fmt.Sprintf("rows[%d]: amount exceeds contract %s headroom", i, row.ContractCode), nil
		}
		runningUsed[row.ContractCode] += row.DisburseAmtMinor
	}
	return true, "", nil
}

// Resolve applies the workflow decision. REJECT/CANCEL are terminal without
// posting — the finance hold release is the batch cancel worker's job.
func (s *BatchDisbursementService) Resolve(ctx context.Context, tenantID, id, decision string) error {
	status := domain.BatchRejected
	if decision == "APPROVE" {
		status = domain.BatchApproved
	}
	if err := s.repo.SetDisbursementBatchStatus(ctx, tenantID, id, status); err != nil {
		return mapRepoError(err)
	}
	return nil
}

// BatchPostingDetail is everything the workflow worker needs to build the
// batch posting request: header + per-row agreement/contract context.
func (s *BatchDisbursementService) BatchPostingDetail(ctx context.Context, tenantID, batchID string) (*loanv1.BatchPostingDetail, error) {
	batch, err := s.repo.GetDisbursementBatch(ctx, tenantID, batchID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	rows, err := s.repo.GetBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	detail := &loanv1.BatchPostingDetail{
		BatchId:         batch.ID,
		BatchCode:       batch.ID,
		BatchType:       batchTypeOfDisb(batch.FlowType),
		TxnDate:         batch.TxnDate,
		PaymentMethod:   batch.PaymentMethod,
		AccountCode:     batch.AccountCode,
		CurrencyCode:    batch.CurrencyCode,
		TotalAmtMinor:   batch.TotalAmtMinor,
		Description:     batch.Description,
		OrgUnitCode:     batch.OrgCode,
		WorkflowCaseId:  "",
		Trader:          batch.Trader,
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
			RowId:         row.ID,
			ContractCode:  row.ContractCode,
			AgreementCode: row.AgreementCode,
			PlanCode:      agreement.PlanCode,
			AmountMinor:   row.DisburseAmtMinor,
			DebtGroupCode: agreement.DebtGroupCode,
			OrgUnitCode:   contract.EmployeeCode,
			CustomerCode:  contract.CustomerCode,
			IsClosed:      row.IsClosed,
		})
	}
	return detail, nil
}

func batchTypeOfDisb(flowType string) string {
	if flowType == domain.FlowComplete {
		return domain.BatchTypeDisbComplete
	}
	return domain.BatchTypeDisbRegister
}

// SettleBatch dispatches per flow_type (worker RPC surface).
func (s *BatchDisbursementService) SettleBatch(ctx context.Context, tenantID, batchID, journalEntryID, actor string) error {
	batch, err := s.repo.GetDisbursementBatch(ctx, tenantID, batchID)
	if err != nil {
		return mapRepoError(err)
	}
	if batch.FlowType == domain.FlowComplete {
		return s.SettleBatchComplete(ctx, tenantID, batchID, journalEntryID, actor)
	}
	return s.SettleBatchRegister(ctx, tenantID, batchID, journalEntryID, actor)
}

// SettleBatchRegister marks the batch + its REGISTER rows POSTED and settles
// each agreement (outstanding + pending bump, PENDING → ACTIVE) — the exact
// per-row side effects of the single-row SettleRegister, looped.
func (s *BatchDisbursementService) SettleBatchRegister(ctx context.Context, tenantID, batchID, journalEntryID, actor string) error {
	rows, err := s.repo.GetBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return mapRepoError(err)
	}
	for _, row := range rows {
		if err := s.repo.SetDisbursementCaseAndJournal(ctx, tenantID, row.ID, "", "", journalEntryID); err != nil {
			return mapRepoError(err)
		}
		if err := s.repo.SetDisbursementStatus(ctx, tenantID, row.ID, domain.DisbursementPosted, actor); err != nil {
			return mapRepoError(err)
		}
		if err := s.repo.SettleRegisterDisbursement(ctx, tenantID, row.AgreementCode, row.DisburseAmtMinor); err != nil {
			return mapRepoError(err)
		}
	}
	return s.repo.SetDisbursementBatchPosted(ctx, tenantID, batchID, journalEntryID)
}

// SettleBatchComplete marks the batch + its COMPLETE rows POSTED, unwinds
// the pending per agreement and activates/closes the contracts. The
// single-row SettleComplete contract activation query keys on any POSTED
// COMPLETE row of the contract, so looping the per-row repo call reproduces
// it; is_closed rows additionally CLOSE the contract.
func (s *BatchDisbursementService) SettleBatchComplete(ctx context.Context, tenantID, batchID, journalEntryID, actor string) error {
	rows, err := s.repo.GetBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return mapRepoError(err)
	}
	closedContracts := []string{}
	for _, row := range rows {
		if err := s.repo.SetDisbursementCaseAndJournal(ctx, tenantID, row.ID, "", "", journalEntryID); err != nil {
			return mapRepoError(err)
		}
		if err := s.repo.SetDisbursementStatus(ctx, tenantID, row.ID, domain.DisbursementPosted, actor); err != nil {
			return mapRepoError(err)
		}
		if err := s.repo.SettleCompleteDisbursement(ctx, tenantID, row.ContractCode, row.AgreementCode, row.DisburseAmtMinor); err != nil {
			return mapRepoError(err)
		}
		if row.IsClosed {
			closedContracts = append(closedContracts, row.ContractCode)
		}
	}
	for _, contractCode := range closedContracts {
		if err := s.repo.CloseContractByCode(ctx, tenantID, contractCode); err != nil {
			return mapRepoError(err)
		}
	}
	return s.repo.SetDisbursementBatchPosted(ctx, tenantID, batchID, journalEntryID)
}
