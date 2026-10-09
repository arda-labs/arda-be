package service

import (
	"context"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
	"strings"
)

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
	history, err := s.repo.GetDisbursementBatchHistory(ctx, tenantID, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	batch.History = history
	return batch, nil
}

// UpdateDraft replaces a saved DRAFT header and rows under a version guard.
func (s *BatchDisbursementService) UpdateDraft(ctx context.Context, tenantID, actor, id string, in *CreateBatchInput) (*domain.DisbursementBatch, error) {
	if in == nil || in.DataVersion <= 0 {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "data_version is required")
	}
	current, err := s.repo.GetDisbursementBatch(ctx, tenantID, id)
	if err != nil {
		return nil, mapRepoError(err)
	}
	if current.Status != domain.BatchDraft {
		return nil, ardaerrors.New(ardaerrors.CodeConflict, "only DRAFT batches can be edited")
	}
	if !isValidISODate(in.TxnDate) {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "txn_date must be YYYY-MM-DD")
	}
	method := strings.ToUpper(strings.TrimSpace(in.PaymentMethod))
	if method == "" {
		method = "TRANSFER"
	}
	if method != "CASH" && method != "TRANSFER" {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, "payment_method must be one of: CASH, TRANSFER")
	}
	rows := make([]domain.Disbursement, 0, len(in.Rows))
	var total int64
	if current.FlowType == domain.FlowRegister {
		for i, row := range in.Rows {
			row.ContractCode, row.AgreementCode = strings.TrimSpace(row.ContractCode), strings.TrimSpace(row.AgreementCode)
			if row.ContractCode == "" || row.AgreementCode == "" || row.AmountMinor <= 0 {
				return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: contract, agreement and positive amount are required", i))
			}
			a, err := s.repo.GetAgreementByCode(ctx, tenantID, row.AgreementCode)
			if err != nil || a.ContractCode != row.ContractCode {
				return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: agreement does not belong to contract", i))
			}
			if _, err := s.repo.GetContractByCode(ctx, tenantID, row.ContractCode); err != nil {
				return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: contract not found", i))
			}
			rows = append(rows, domain.Disbursement{TenantID: tenantID, ContractCode: row.ContractCode, AgreementCode: row.AgreementCode, DisburseDate: in.TxnDate, DisburseAmtMinor: row.AmountMinor, CurrencyCode: current.CurrencyCode, FlowType: current.FlowType, OrgCode: current.OrgCode, CreatedBy: actor, Status: domain.DisbursementDraft})
			total += row.AmountMinor
		}
	} else {
		sourceRows, err := s.repo.GetBatchRows(ctx, tenantID, current.SourceBatchID)
		if err != nil {
			return nil, mapRepoError(err)
		}
		sourceByAgreement := make(map[string]domain.Disbursement, len(sourceRows))
		for _, row := range sourceRows {
			sourceByAgreement[row.AgreementCode] = row
		}
		for i, row := range in.Rows {
			source, ok := sourceByAgreement[strings.TrimSpace(row.AgreementCode)]
			if !ok || row.AmountMinor < 0 {
				return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: invalid source agreement or amount", i))
			}
			if row.IsClosed {
				row.AmountMinor = 0
			}
			if row.ContractCode != "" && row.ContractCode != source.ContractCode {
				return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: contract does not match source agreement", i))
			}
			rows = append(rows, domain.Disbursement{TenantID: tenantID, ContractCode: source.ContractCode, AgreementCode: source.AgreementCode, DisburseDate: in.TxnDate, DisburseAmtMinor: row.AmountMinor, CurrencyCode: current.CurrencyCode, FlowType: current.FlowType, SourceRegisterID: source.ID, IsClosed: row.IsClosed, OrgCode: current.OrgCode, CreatedBy: actor, Status: domain.DisbursementDraft})
			total += row.AmountMinor
		}
	}
	updated := &domain.DisbursementBatch{TxnDate: in.TxnDate, PaymentMethod: method, AccountCode: in.AccountCode, Description: in.Description, Trader: in.Trader, TotalAmtMinor: total, Rows: rows}
	if err := s.repo.UpdateDraftDisbursementBatch(ctx, tenantID, id, actor, in.DataVersion, updated); err != nil {
		return nil, mapRepoError(err)
	}
	return s.Get(ctx, tenantID, id)
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

// DisbursementBatchHeadroom reports contract-level exposure before and after
// this batch. Pending batches already have HELD reservations; drafts do not.
// Using the same exposure query as submit keeps the checker view consistent
// with the server's authoritative limit calculation.
type DisbursementBatchHeadroom struct {
	ContractCode    string `json:"contract_code"`
	AvailableBefore int64  `json:"available_before_minor"`
	RemainingAfter  int64  `json:"remaining_after_minor"`
}

func (s *BatchDisbursementService) BatchHeadroom(ctx context.Context, tenantID, batchID string) ([]DisbursementBatchHeadroom, error) {
	batch, err := s.repo.GetDisbursementBatch(ctx, tenantID, batchID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	rows, err := s.repo.GetBatchRows(ctx, tenantID, batchID)
	if err != nil {
		return nil, mapRepoError(err)
	}
	exposures := make(map[string]domain.ContractExposure, len(rows))
	for _, row := range rows {
		if _, exists := exposures[row.ContractCode]; exists {
			continue
		}
		exposure, err := s.repo.GetContractExposure(ctx, tenantID, row.ContractCode)
		if err != nil {
			return nil, mapRepoError(err)
		}
		exposures[row.ContractCode] = exposure
	}
	return calculateBatchHeadroom(batch.Status, rows, exposures), nil
}

func calculateBatchHeadroom(status string, rows []domain.Disbursement, exposures map[string]domain.ContractExposure) []DisbursementBatchHeadroom {
	totals := make(map[string]int64, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, exists := totals[row.ContractCode]; !exists {
			order = append(order, row.ContractCode)
		}
		totals[row.ContractCode] += row.DisburseAmtMinor
	}
	result := make([]DisbursementBatchHeadroom, 0, len(order))
	for _, contractCode := range order {
		remaining := exposures[contractCode].HeadroomMinor()
		available := remaining
		if status == domain.BatchDraft {
			remaining -= totals[contractCode]
		} else if status == domain.BatchSubmitted {
			available += totals[contractCode]
		}
		result = append(result, DisbursementBatchHeadroom{
			ContractCode:    contractCode,
			AvailableBefore: available,
			RemainingAfter:  remaining,
		})
	}
	return result
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
