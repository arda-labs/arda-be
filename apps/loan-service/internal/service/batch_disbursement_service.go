package service

import (
	"context"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
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
	PlanCode    string `json:"plan_code,omitempty"`
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

// Check validates the batch is actionable (BPMN validate job): PENDING_APPROVAL and
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
		exposure, err := s.repo.GetContractExposure(ctx, tenantID, row.ContractCode)
		if err != nil {
			return false, mapRepoError(err).Error(), nil
		}
		if exposure.HeadroomMinor() < 0 {
			return false, fmt.Sprintf("rows[%d]: contract %s exposure exceeds its loan amount", i, row.ContractCode), nil
		}
	}
	return true, "", nil
}

// Resolve applies the workflow decision. REJECT/CANCEL are terminal without
// posting — the finance hold release is the batch cancel worker's job.
func (s *BatchDisbursementService) Resolve(ctx context.Context, tenantID, id, decision, note string) error {
	batch, err := s.repo.GetDisbursementBatch(ctx, tenantID, id)
	if err != nil {
		return mapRepoError(err)
	}
	var status string
	switch decision {
	case "APPROVE":
		status = domain.BatchApproved
	case "REJECT":
		status = domain.BatchRejected
	case "CANCEL":
		status = domain.BatchCancelled
	default:
		return ardaerrors.New(ardaerrors.CodeInvalidInput, "unknown decision "+decision)
	}
	if err := domain.CanTransition(domain.WorkflowMachine, domain.Status(batch.Status), domain.Status(status), note); err != nil {
		return mapRepoError(err)
	}
	if err := s.repo.SetDisbursementBatchStatus(ctx, tenantID, id, batch.Status, status, note); err != nil {
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
		BatchId:        batch.ID,
		BatchCode:      batch.ID,
		BatchType:      batchTypeOfDisb(batch.FlowType),
		TxnDate:        batch.TxnDate,
		PaymentMethod:  batch.PaymentMethod,
		AccountCode:    batch.AccountCode,
		CurrencyCode:   batch.CurrencyCode,
		TotalAmtMinor:  batch.TotalAmtMinor,
		Description:    batch.Description,
		OrgUnitCode:    batch.OrgCode,
		WorkflowCaseId: "",
		Trader:         batch.Trader,
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
	batch, err := s.repo.GetDisbursementBatch(ctx, tenantID, batchID)
	if err != nil {
		return mapRepoError(err)
	}
	if batch.Status == domain.BatchPosted {
		return nil
	}
	if batch.Status == domain.BatchSubmitted {
		if err := s.repo.SetDisbursementBatchStatus(ctx, tenantID, batchID, batch.Status, domain.BatchApproved, ""); err != nil {
			return mapRepoError(err)
		}
	}
	rows, err := s.repo.GetBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return mapRepoError(err)
	}
	// The guard belongs to the batch header, not its rows: settle each row
	// unguarded (row-level idempotency is the status guard) and guard the
	// header transition below.
	rowCtx := domain.WithDataVersion(ctx, 0)
	for _, row := range rows {
		if _, err := s.repo.SettleDisbursementRegister(rowCtx, tenantID, row.ID, row.AgreementCode, row.DisburseAmtMinor, journalEntryID, actor); err != nil {
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
	batch, err := s.repo.GetDisbursementBatch(ctx, tenantID, batchID)
	if err != nil {
		return mapRepoError(err)
	}
	if batch.Status == domain.BatchPosted {
		return nil
	}
	if batch.Status == domain.BatchSubmitted {
		if err := s.repo.SetDisbursementBatchStatus(ctx, tenantID, batchID, batch.Status, domain.BatchApproved, ""); err != nil {
			return mapRepoError(err)
		}
	}
	rows, err := s.repo.GetBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return mapRepoError(err)
	}
	closedContracts := []string{}
	rowCtx := domain.WithDataVersion(ctx, 0)
	for _, row := range rows {
		if _, err := s.repo.SettleDisbursementComplete(rowCtx, tenantID, row.ID, row.ContractCode, row.AgreementCode, row.DisburseAmtMinor, journalEntryID, actor); err != nil {
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
