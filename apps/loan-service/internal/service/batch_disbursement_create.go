package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"strings"
)

// CreateBatchRegister creates a DRAFT batch REGISTER dossier and its rows in
// one tx. Workflow submission and contract reservations happen only when the
// maker explicitly submits the saved draft.
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
		TenantID:      tenantID,
		OrgCode:       orgCode,
		FlowType:      domain.FlowRegister,
		TxnDate:       in.TxnDate,
		PaymentMethod: paymentMethod,
		AccountCode:   in.AccountCode,
		CurrencyCode:  "VND",
		Description:   in.Description,
		Trader:        in.Trader,
		Status:        domain.BatchDraft,
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
		if _, err := s.repo.GetContractByCode(ctx, tenantID, row.ContractCode); err != nil {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, fmt.Sprintf("rows[%d]: contract_code not found: %s", i, row.ContractCode))
		}
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
		if errors.Is(err, repository.ErrHeadroomExceeded) {
			return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
		}
		return nil, mapRepoError(err)
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
		Status:        domain.BatchDraft,
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
