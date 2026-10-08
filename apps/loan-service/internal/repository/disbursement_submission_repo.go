package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

type DisbursementWorkflowOutbox struct {
	TenantID     string
	BatchID      string
	AttemptCount int
}

// SubmitDisbursementBatch validates and reserves a saved batch in one
// transaction, transitions it to pending approval, and enqueues workflow work.
func (r *LoanRepository) SubmitDisbursementBatch(ctx context.Context, tenantID, id, actor string, expectedVersion int64) (*domain.DisbursementBatch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	batch, err := scanDisbursementBatch(tx.QueryRowContext(ctx, `
		SELECT `+disbursementBatchColumns+`
		FROM lnm_disbursement_batches WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, id))
	if err != nil {
		return nil, mapNoRows(err)
	}
	if batch.Status == domain.BatchSubmitted {
		_ = tx.Rollback()
		return &batch, nil
	}
	if batch.Status != domain.BatchDraft && batch.Status != domain.BatchSubmitFailed {
		return nil, fmt.Errorf("%w: batch status %s cannot be submitted", domain.ErrInvalidTransition, batch.Status)
	}
	if expectedVersion <= 0 || batch.DataVersion != expectedVersion {
		return nil, ErrStaleVersion
	}
	if err := domain.CanTransition(domain.WorkflowMachine, domain.Status(batch.Status), domain.Status(domain.BatchSubmitted), ""); err != nil {
		return nil, err
	}
	rows, err := getDisbursementBatchRows(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: disbursement batch has no rows", ErrConflict)
	}
	if batch.FlowType == domain.FlowRegister {
		if err := validateAndReserveRegisterBatch(ctx, tx, tenantID, rows); err != nil {
			return nil, err
		}
	} else if batch.FlowType == domain.FlowComplete {
		if err := validateCompleteBatch(ctx, tx, tenantID, batch.SourceBatchID, rows); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("%w: unsupported disbursement flow %q", ErrConflict, batch.FlowType)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches
		SET status = 'PENDING_APPROVAL', updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = $3 AND version = $4`, tenantID, id, batch.Status, expectedVersion)
	if err != nil {
		return nil, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return nil, ErrStaleVersion
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lnm_disbursements SET status = 'PENDING_APPROVAL', updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND batch_id = $2 AND status = 'DRAFT'`, tenantID, id); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO lnm_disbursement_workflow_outbox (tenant_id, batch_id, status, attempt_count, next_attempt_at, last_error, updated_at)
		VALUES ($1,$2,'PENDING',0,now(),'',now())
		ON CONFLICT (tenant_id, batch_id) DO UPDATE
		SET status = 'PENDING', attempt_count = 0, next_attempt_at = now(), locked_at = NULL,
		    last_error = '', delivered_at = NULL, updated_at = now()`, tenantID, id); err != nil {
		return nil, err
	}
	if err := insertDisbursementBatchHistory(ctx, tx, tenantID, id, "SUBMITTED", batch.Status, domain.BatchSubmitted, "Workflow submission queued", actor); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetDisbursementBatch(ctx, tenantID, id)
}

func validateAndReserveRegisterBatch(ctx context.Context, tx *sql.Tx, tenantID string, rows []domain.Disbursement) error {
	agreementCodes := make([]string, 0, len(rows))
	contractCodes := make([]string, 0, len(rows))
	requested := make(map[string]int64, len(rows))
	seenAgreements := make(map[string]struct{}, len(rows))
	seenContracts := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.DisburseAmtMinor <= 0 {
			return fmt.Errorf("%w: batch row amount must be positive", ErrConflict)
		}
		if _, exists := seenAgreements[row.AgreementCode]; !exists {
			seenAgreements[row.AgreementCode] = struct{}{}
			agreementCodes = append(agreementCodes, row.AgreementCode)
		}
		if _, exists := seenContracts[row.ContractCode]; !exists {
			seenContracts[row.ContractCode] = struct{}{}
			contractCodes = append(contractCodes, row.ContractCode)
		}
		requested[row.ContractCode] += row.DisburseAmtMinor
	}
	sort.Strings(contractCodes)
	sort.Strings(agreementCodes)
	locked := make(map[string]struct{}, len(contractCodes))
	contracts, err := tx.QueryContext(ctx, `
		SELECT contract_code FROM lnm_contracts
		WHERE tenant_id = $1 AND contract_code = ANY($2::varchar[])
		ORDER BY contract_code FOR UPDATE`, tenantID, contractCodes)
	if err != nil {
		return err
	}
	for contracts.Next() {
		var code string
		if err := contracts.Scan(&code); err != nil {
			contracts.Close()
			return err
		}
		locked[code] = struct{}{}
	}
	if err := contracts.Err(); err != nil {
		contracts.Close()
		return err
	}
	contracts.Close()
	if len(locked) != len(contractCodes) {
		return ErrNotFound
	}
	agreementOwners := make(map[string]string, len(agreementCodes))
	agreements, err := tx.QueryContext(ctx, `
		SELECT agreement_code, contract_code FROM lnm_agreements
		WHERE tenant_id = $1 AND agreement_code = ANY($2::varchar[])`, tenantID, agreementCodes)
	if err != nil {
		return err
	}
	for agreements.Next() {
		var agreementCode, contractCode string
		if err := agreements.Scan(&agreementCode, &contractCode); err != nil {
			agreements.Close()
			return err
		}
		agreementOwners[agreementCode] = contractCode
	}
	if err := agreements.Err(); err != nil {
		agreements.Close()
		return err
	}
	agreements.Close()
	if len(agreementOwners) != len(agreementCodes) {
		return ErrNotFound
	}
	for _, row := range rows {
		if agreementOwners[row.AgreementCode] != row.ContractCode {
			return fmt.Errorf("%w: agreement %s does not belong to contract %s", ErrConflict, row.AgreementCode, row.ContractCode)
		}
	}
	exposures := make(map[string]domain.ContractExposure, len(contractCodes))
	exposureRows, err := tx.QueryContext(ctx, `
		SELECT c.contract_code, c.loan_amt_minor,
		       COALESCE(a.outstanding_minor,0), COALESCE(a.pending_minor,0), COALESCE(r.reserved_minor,0)
		FROM lnm_contracts c
		LEFT JOIN (
			SELECT contract_code, SUM(outstanding_amt_minor) AS outstanding_minor,
			       SUM(pending_disburse_amt_minor) AS pending_minor
			FROM lnm_agreements WHERE tenant_id = $1 AND contract_code = ANY($2::varchar[])
			GROUP BY contract_code
		) a ON a.contract_code = c.contract_code
		LEFT JOIN (
			SELECT contract_code, SUM(amount_minor) AS reserved_minor
			FROM lnm_contract_reservations WHERE tenant_id = $1 AND status = 'HELD'
			  AND contract_code = ANY($2::varchar[])
			GROUP BY contract_code
		) r ON r.contract_code = c.contract_code
		WHERE c.tenant_id = $1 AND c.contract_code = ANY($2::varchar[])`, tenantID, contractCodes)
	if err != nil {
		return err
	}
	for exposureRows.Next() {
		var code string
		var exposure domain.ContractExposure
		if err := exposureRows.Scan(&code, &exposure.LoanAmountMinor, &exposure.OutstandingMinor, &exposure.PendingMinor, &exposure.ReservedMinor); err != nil {
			exposureRows.Close()
			return err
		}
		exposures[code] = exposure
	}
	if err := exposureRows.Err(); err != nil {
		exposureRows.Close()
		return err
	}
	exposureRows.Close()
	if len(exposures) != len(contractCodes) {
		return ErrNotFound
	}
	for code, amount := range requested {
		exposure := exposures[code]
		if !exposure.Allows(amount) {
			return fmt.Errorf("%w: amount_minor %d exceeds contract %s headroom %d", ErrHeadroomExceeded, amount, code, exposure.HeadroomMinor())
		}
	}
	contractArray := make([]string, 0, len(rows))
	rowIDs := make([]string, 0, len(rows))
	amounts := make([]int64, 0, len(rows))
	for _, row := range rows {
		contractArray = append(contractArray, row.ContractCode)
		rowIDs = append(rowIDs, row.ID)
		amounts = append(amounts, row.DisburseAmtMinor)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO lnm_contract_reservations
			(tenant_id, contract_code, source_type, source_id, amount_minor, status)
		SELECT $1, input.contract_code, 'DISBURSEMENT', input.source_id, input.amount_minor, 'HELD'
		FROM unnest($2::varchar[], $3::uuid[], $4::bigint[]) AS input(contract_code, source_id, amount_minor)
		ON CONFLICT (source_type, source_id) DO UPDATE
		SET contract_code = EXCLUDED.contract_code, amount_minor = EXCLUDED.amount_minor, status = 'HELD'`,
		tenantID, contractArray, rowIDs, amounts)
	return err
}

func validateCompleteBatch(ctx context.Context, tx *sql.Tx, tenantID, sourceBatchID string, rows []domain.Disbursement) error {
	if sourceBatchID == "" {
		return fmt.Errorf("%w: complete batch requires a source register", ErrConflict)
	}
	var sourceStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM lnm_disbursement_batches WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, sourceBatchID).Scan(&sourceStatus); err != nil {
		return mapNoRows(err)
	}
	if sourceStatus != domain.BatchPosted {
		return fmt.Errorf("%w: source register batch is not POSTED", ErrConflict)
	}
	agreementCodes := make([]string, 0, len(rows))
	sourceIDs := make([]string, 0, len(rows))
	requested := make(map[string]int64, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.SourceRegisterID == "" || row.DisburseAmtMinor < 0 {
			return fmt.Errorf("%w: invalid complete batch row", ErrConflict)
		}
		if _, ok := seen[row.AgreementCode]; !ok {
			seen[row.AgreementCode] = struct{}{}
			agreementCodes = append(agreementCodes, row.AgreementCode)
		}
		requested[row.SourceRegisterID] += row.DisburseAmtMinor
		sourceIDs = append(sourceIDs, row.SourceRegisterID)
	}
	sort.Strings(agreementCodes)
	lockedAgreements, err := tx.QueryContext(ctx, `
		SELECT agreement_code FROM lnm_agreements
		WHERE tenant_id=$1 AND agreement_code=ANY($2::varchar[])
		ORDER BY agreement_code FOR UPDATE`, tenantID, agreementCodes)
	if err != nil {
		return err
	}
	lockedCount := 0
	for lockedAgreements.Next() {
		var agreementCode string
		if err := lockedAgreements.Scan(&agreementCode); err != nil {
			lockedAgreements.Close()
			return err
		}
		lockedCount++
	}
	if err := lockedAgreements.Err(); err != nil {
		lockedAgreements.Close()
		return err
	}
	lockedAgreements.Close()
	if lockedCount != len(agreementCodes) {
		return ErrNotFound
	}
	type sourceRegister struct {
		agreementCode string
		amountMinor   int64
	}
	sourceAmounts := make(map[string]sourceRegister, len(rows))
	sourceRows, err := tx.QueryContext(ctx, `
		SELECT id::text, agreement_code, disburse_amt_minor
		FROM lnm_disbursements
		WHERE tenant_id=$1 AND batch_id=$2 AND flow_type='REGISTER' AND status='POSTED'
		  AND id=ANY($3::uuid[])`, tenantID, sourceBatchID, sourceIDs)
	if err != nil {
		return err
	}
	for sourceRows.Next() {
		var sourceID, agreementCode string
		var amount int64
		if err := sourceRows.Scan(&sourceID, &agreementCode, &amount); err != nil {
			sourceRows.Close()
			return err
		}
		sourceAmounts[sourceID] = sourceRegister{agreementCode: agreementCode, amountMinor: amount}
	}
	if err := sourceRows.Err(); err != nil {
		sourceRows.Close()
		return err
	}
	sourceRows.Close()
	if len(sourceAmounts) != len(requested) {
		return ErrNotFound
	}
	for _, row := range rows {
		if sourceAmounts[row.SourceRegisterID].agreementCode != row.AgreementCode {
			return fmt.Errorf("%w: complete row agreement does not match source register row", ErrConflict)
		}
	}
	completed := make(map[string]int64, len(rows))
	completedRows, err := tx.QueryContext(ctx, `
		SELECT source_register_id::text, COALESCE(SUM(disburse_amt_minor),0)
		FROM lnm_disbursements
		WHERE tenant_id=$1 AND flow_type='COMPLETE' AND status IN ('PENDING_APPROVAL','APPROVED','POSTED')
		  AND source_register_id=ANY($2::uuid[])
		GROUP BY source_register_id`, tenantID, sourceIDs)
	if err != nil {
		return err
	}
	for completedRows.Next() {
		var sourceID string
		var amount int64
		if err := completedRows.Scan(&sourceID, &amount); err != nil {
			completedRows.Close()
			return err
		}
		completed[sourceID] = amount
	}
	if err := completedRows.Err(); err != nil {
		completedRows.Close()
		return err
	}
	completedRows.Close()
	for sourceID, amount := range requested {
		if amount > sourceAmounts[sourceID].amountMinor-completed[sourceID] {
			return fmt.Errorf("%w: complete batch exceeds source register remainder", ErrHeadroomExceeded)
		}
	}
	return nil
}

func getDisbursementBatchRows(ctx context.Context, q repoTX, tenantID, batchID string) ([]domain.Disbursement, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id::text, tenant_id, contract_code, agreement_code, disburse_date::text, disburse_amt_minor,
		       currency_code, COALESCE(fund_source_code,''), flow_type, COALESCE(source_register_id::text,''), status,
		       workflow_case_id::text, COALESCE(workflow_case_code,''), journal_entry_id::text, COALESCE(batch_id::text,''), is_closed,
		       created_by, created_at, updated_at
		FROM lnm_disbursements WHERE tenant_id = $1 AND batch_id = $2 ORDER BY created_at, id`, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Disbursement, 0)
	for rows.Next() {
		var row domain.Disbursement
		var caseID, entryID, sourceID sql.NullString
		if err := rows.Scan(&row.ID, &row.TenantID, &row.ContractCode, &row.AgreementCode, &row.DisburseDate,
			&row.DisburseAmtMinor, &row.CurrencyCode, &row.FundSourceCode, &row.FlowType, &sourceID, &row.Status,
			&caseID, &row.WorkflowCaseCode, &entryID, &row.BatchID, &row.IsClosed,
			&row.CreatedBy, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		if sourceID.Valid {
			row.SourceRegisterID = sourceID.String
		}
		if caseID.Valid {
			row.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			row.JournalEntryID = &entryID.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ClaimDisbursementWorkflowOutbox claims queued or expired leased submissions.
func (r *LoanRepository) ClaimDisbursementWorkflowOutbox(ctx context.Context, limit int) ([]DisbursementWorkflowOutbox, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT tenant_id, batch_id::text, attempt_count
		FROM lnm_disbursement_workflow_outbox
		WHERE (status='PENDING' AND next_attempt_at <= now())
		   OR (status='PROCESSING' AND locked_at < now() - interval '1 minute')
		ORDER BY created_at, id
		FOR UPDATE SKIP LOCKED LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	items := make([]DisbursementWorkflowOutbox, 0)
	for rows.Next() {
		var item DisbursementWorkflowOutbox
		if err := rows.Scan(&item.TenantID, &item.BatchID, &item.AttemptCount); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE lnm_disbursement_workflow_outbox SET status='PROCESSING', locked_at=now(), updated_at=now() WHERE tenant_id=$1 AND batch_id=$2`, item.TenantID, item.BatchID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *LoanRepository) CompleteDisbursementWorkflowOutbox(ctx context.Context, item DisbursementWorkflowOutbox, caseID, caseCode string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches
		SET workflow_case_id=$3, workflow_case_code=$4, updated_at=now(), version=version+1
		WHERE tenant_id=$1 AND id=$2 AND status='PENDING_APPROVAL'`, item.TenantID, item.BatchID, nullText(caseID), caseCode)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrStaleVersion
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE lnm_disbursement_workflow_outbox
		SET status='DELIVERED', delivered_at=now(), locked_at=NULL, last_error='', updated_at=now()
		WHERE tenant_id=$1 AND batch_id=$2 AND status='PROCESSING'`, item.TenantID, item.BatchID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrStaleVersion
	}
	if err := insertDisbursementBatchHistory(ctx, tx, item.TenantID, item.BatchID, "WORKFLOW_CASE_CREATED", domain.BatchSubmitted, domain.BatchSubmitted, "Workflow case started", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *LoanRepository) FailDisbursementWorkflowOutbox(ctx context.Context, item DisbursementWorkflowOutbox, cause string, maxAttempts int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attempts int
	var state string
	if err := tx.QueryRowContext(ctx, `
		UPDATE lnm_disbursement_workflow_outbox
		SET attempt_count=attempt_count+1,
		    status=CASE WHEN attempt_count+1 >= $3 THEN 'DEAD' ELSE 'PENDING' END,
		    next_attempt_at=now() + LEAST(interval '5 minutes', interval '1 second' * power(2, LEAST(attempt_count, 8))),
		    locked_at=NULL, last_error=$4, updated_at=now()
		WHERE tenant_id=$1 AND batch_id=$2 AND status='PROCESSING'
		RETURNING attempt_count,status`, item.TenantID, item.BatchID, maxAttempts, cause).Scan(&attempts, &state); err != nil {
		return err
	}
	if state == "DEAD" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE lnm_disbursement_batches SET status='SUBMIT_FAILED', updated_at=now(), version=version+1
			WHERE tenant_id=$1 AND id=$2 AND status='PENDING_APPROVAL'`, item.TenantID, item.BatchID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE lnm_disbursements SET status='DRAFT', updated_at=now(), version=version+1
			WHERE tenant_id=$1 AND batch_id=$2 AND status='PENDING_APPROVAL'`, item.TenantID, item.BatchID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE lnm_contract_reservations r SET status='RELEASED'
			WHERE r.tenant_id=$1 AND r.source_type='DISBURSEMENT' AND r.status='HELD'
			  AND r.source_id IN (SELECT d.id FROM lnm_disbursements d WHERE d.tenant_id=$1 AND d.batch_id=$2)`, item.TenantID, item.BatchID); err != nil {
			return err
		}
		if err := insertDisbursementBatchHistory(ctx, tx, item.TenantID, item.BatchID, "SUBMISSION_FAILED", domain.BatchSubmitted, domain.BatchSubmitFailed, cause, ""); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `
		INSERT INTO lnm_disbursement_batch_history
			(tenant_id,batch_id,event_type,from_status,to_status,detail)
		VALUES ($1,$2,'WORKFLOW_RETRY', 'PENDING_APPROVAL','PENDING_APPROVAL',$3)`, item.TenantID, item.BatchID, fmt.Sprintf("attempt %d: %s", attempts, cause)); err != nil {
		return err
	}
	return tx.Commit()
}
