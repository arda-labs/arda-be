package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

const disbursementBatchColumns = `id::text, tenant_id, COALESCE(org_code,''), flow_type, COALESCE(source_batch_id::text,''),
	txn_date::text, COALESCE(payment_method,''), COALESCE(account_code,''), COALESCE(currency_code,''), total_amt_minor,
	COALESCE(description,''), trader, status, workflow_case_id::text, COALESCE(workflow_case_code,''),
	journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at, version`

func scanDisbursementBatch(s interface{ Scan(...any) error }) (domain.DisbursementBatch, error) {
	var b domain.DisbursementBatch
	var caseID, entryID sql.NullString
	var trader []byte
	err := s.Scan(&b.ID, &b.TenantID, &b.OrgCode, &b.FlowType, &b.SourceBatchID,
		&b.TxnDate, &b.PaymentMethod, &b.AccountCode, &b.CurrencyCode, &b.TotalAmtMinor,
		&b.Description, &trader, &b.Status, &caseID, &b.WorkflowCaseCode,
		&entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.DataVersion)
	if err != nil {
		return b, err
	}
	b.Trader = map[string]string{}
	if len(trader) > 0 {
		_ = json.Unmarshal(trader, &b.Trader)
	}
	if caseID.Valid {
		b.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		b.JournalEntryID = &entryID.String
	}
	return b, nil
}

// CreateDisbursementBatch inserts the batch header and its disbursement rows
// in one transaction — a batch is never partially visible.
func (r *LoanRepository) CreateDisbursementBatch(ctx context.Context, b *domain.DisbursementBatch) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	trader, err := json.Marshal(b.Trader)
	if err != nil {
		return err
	}
	if len(b.Trader) == 0 {
		trader = []byte("{}")
	}
	var sourceID any
	if b.SourceBatchID != "" {
		sourceID = b.SourceBatchID
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO lnm_disbursement_batches
			(tenant_id, org_code, flow_type, source_batch_id, txn_date, payment_method, account_code,
			 currency_code, total_amt_minor, description, trader, status, created_by)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11::jsonb,$12,$13)
		RETURNING id::text, created_at, updated_at, version`,
		b.TenantID, b.OrgCode, b.FlowType, sourceID, b.TxnDate, b.PaymentMethod, b.AccountCode,
		b.CurrencyCode, b.TotalAmtMinor, b.Description, string(trader), b.Status, b.CreatedBy).
		Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt, &b.DataVersion)
	if err != nil {
		return err
	}
	for i := range b.Rows {
		b.Rows[i].BatchID = b.ID
		if err := insertDisbursementBatchRow(ctx, tx, &b.Rows[i]); err != nil {
			return err
		}
	}
	if err := insertDisbursementBatchHistory(ctx, tx, b.TenantID, b.ID, "CREATED", "", domain.BatchDraft, "", b.CreatedBy); err != nil {
		return err
	}
	return tx.Commit()
}

func insertDisbursementBatchRow(ctx context.Context, q repoTX, row *domain.Disbursement) error {
	return q.QueryRowContext(ctx, `
		INSERT INTO lnm_disbursements
			(tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor,
			 currency_code, flow_type, source_register_id, batch_id, is_closed, status, org_code, created_by)
		VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,'DRAFT',$11,$12)
		RETURNING id::text`,
		row.TenantID, row.ContractCode, row.AgreementCode, row.DisburseDate, row.DisburseAmtMinor,
		row.CurrencyCode, row.FlowType, nullText(row.SourceRegisterID), row.BatchID, row.IsClosed,
		row.OrgCode, row.CreatedBy).Scan(&row.ID)
}

func insertDisbursementBatchHistory(ctx context.Context, q repoTX, tenantID, batchID, eventType, fromStatus, toStatus, detail, actor string) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO lnm_disbursement_batch_history
			(tenant_id, batch_id, event_type, from_status, to_status, detail, actor)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, tenantID, batchID, eventType, fromStatus, toStatus, detail, actor)
	return err
}

// GetDisbursementBatchHistory returns the append-only audit timeline.
func (r *LoanRepository) GetDisbursementBatchHistory(ctx context.Context, tenantID, batchID string) ([]domain.DisbursementBatchEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT event_type, from_status, to_status, detail, actor, created_at
		FROM lnm_disbursement_batch_history
		WHERE tenant_id = $1 AND batch_id = $2
		ORDER BY created_at, id`, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]domain.DisbursementBatchEvent, 0)
	for rows.Next() {
		var event domain.DisbursementBatchEvent
		if err := rows.Scan(&event.EventType, &event.FromStatus, &event.ToStatus, &event.Detail, &event.Actor, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// UpdateDraftDisbursementBatch replaces the editable header and rows under a
// tenant, status, and optimistic-version guard.
func (r *LoanRepository) UpdateDraftDisbursementBatch(ctx context.Context, tenantID, id, actor string, expectedVersion int64, b *domain.DisbursementBatch) error {
	trader, err := json.Marshal(b.Trader)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches
		SET txn_date = $4::date, payment_method = $5, account_code = $6, total_amt_minor = $7,
		    description = $8, trader = $9::jsonb, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'DRAFT' AND version = $3`,
		tenantID, id, expectedVersion, b.TxnDate, b.PaymentMethod, b.AccountCode, b.TotalAmtMinor, b.Description, string(trader))
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrStaleVersion
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM lnm_disbursements WHERE tenant_id = $1 AND batch_id = $2 AND status = 'DRAFT'`, tenantID, id); err != nil {
		return err
	}
	for i := range b.Rows {
		b.Rows[i].BatchID = id
		if err := insertDisbursementBatchRow(ctx, tx, &b.Rows[i]); err != nil {
			return err
		}
	}
	if err := insertDisbursementBatchHistory(ctx, tx, tenantID, id, "UPDATED", domain.BatchDraft, domain.BatchDraft, "", actor); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelDraftDisbursementBatch retains rows and history while cancelling an
// unsubmitted draft.
func (r *LoanRepository) CancelDraftDisbursementBatch(ctx context.Context, tenantID, id, actor string, expectedVersion int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches SET status = 'CANCELLED', updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'DRAFT' AND version = $3`, tenantID, id, expectedVersion)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrStaleVersion
	}
	if _, err := tx.ExecContext(ctx, `UPDATE lnm_disbursements SET status = 'CANCELLED', updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND batch_id = $2 AND status = 'DRAFT'`, tenantID, id); err != nil {
		return err
	}
	if err := insertDisbursementBatchHistory(ctx, tx, tenantID, id, "CANCELLED", domain.BatchDraft, domain.BatchCancelled, "", actor); err != nil {
		return err
	}
	return tx.Commit()
}

// ListDisbursementBatches is the paged batch list (status/flow whitelisted
// exact filters).
func (r *LoanRepository) ListDisbursementBatches(ctx context.Context, tenantID, status, flowType string, limit, offset int) ([]domain.DisbursementBatch, int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+disbursementBatchColumns+`, count(*) OVER() AS total_count
		FROM lnm_disbursement_batches
		WHERE tenant_id = $1
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR flow_type = $3)
		ORDER BY created_at DESC, id
		LIMIT $4 OFFSET $5`, tenantID, status, flowType, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.DisbursementBatch{}
	total := 0
	for rows.Next() {
		b, totalCount, err := scanDisbursementBatchTotal(rows)
		if err != nil {
			return nil, 0, err
		}
		total = totalCount
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// scanDisbursementBatchTotal scans one list row: batch columns + window total.
func scanDisbursementBatchTotal(s interface{ Scan(...any) error }) (domain.DisbursementBatch, int, error) {
	var b domain.DisbursementBatch
	var caseID, entryID sql.NullString
	var trader []byte
	var total int
	err := s.Scan(&b.ID, &b.TenantID, &b.OrgCode, &b.FlowType, &b.SourceBatchID,
		&b.TxnDate, &b.PaymentMethod, &b.AccountCode, &b.CurrencyCode, &b.TotalAmtMinor,
		&b.Description, &trader, &b.Status, &caseID, &b.WorkflowCaseCode,
		&entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.DataVersion, &total)
	if err != nil {
		return b, 0, err
	}
	b.Trader = map[string]string{}
	if len(trader) > 0 {
		_ = json.Unmarshal(trader, &b.Trader)
	}
	if caseID.Valid {
		b.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		b.JournalEntryID = &entryID.String
	}
	return b, total, nil
}

// GetDisbursementBatch loads one batch header.
func (r *LoanRepository) GetDisbursementBatch(ctx context.Context, tenantID, id string) (*domain.DisbursementBatch, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+disbursementBatchColumns+`
		FROM lnm_disbursement_batches WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	b, err := scanDisbursementBatch(row)
	if err != nil {
		return nil, mapNoRows(err)
	}
	return &b, nil
}

// GetBatchRows returns the disbursement rows of one batch (order preserved
// by creation).
func (r *LoanRepository) GetBatchRows(ctx context.Context, tenantID, batchID string) ([]domain.Disbursement, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, contract_code, agreement_code, disburse_date::text, disburse_amt_minor,
		       currency_code, COALESCE(fund_source_code,''), flow_type, COALESCE(source_register_id::text,''), status,
		       workflow_case_id::text, COALESCE(workflow_case_code,''), journal_entry_id::text, COALESCE(batch_id::text,''), is_closed,
		       created_by, created_at, updated_at
		FROM lnm_disbursements
		WHERE tenant_id = $1 AND batch_id = $2
		ORDER BY created_at, id`, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Disbursement{}
	for rows.Next() {
		var d domain.Disbursement
		var caseID, entryID, sourceID sql.NullString
		if err := rows.Scan(&d.ID, &d.TenantID, &d.ContractCode, &d.AgreementCode, &d.DisburseDate,
			&d.DisburseAmtMinor, &d.CurrencyCode, &d.FundSourceCode, &d.FlowType, &sourceID, &d.Status,
			&caseID, &d.WorkflowCaseCode, &entryID, &d.BatchID, &d.IsClosed,
			&d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if caseID.Valid {
			d.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			d.JournalEntryID = &entryID.String
		}
		if sourceID.Valid {
			d.SourceRegisterID = sourceID.String
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDisbursementBatchCase records the workflow case on the batch.
func (r *LoanRepository) SetDisbursementBatchCase(ctx context.Context, tenantID, id, caseID, caseCode string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches
		SET workflow_case_id = $3, workflow_case_code = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nullText(caseID), caseCode)
	return err
}

// SetDisbursementBatchStatus transitions the batch status.
func (r *LoanRepository) SetDisbursementBatchStatus(ctx context.Context, tenantID, id, from, to, reason string) error {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.Status(from), domain.Status(to), reason); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setDisbursementBatchStatus(ctx, tx, tenantID, id, from, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lnm_disbursements SET status = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND batch_id = $2 AND status = $3`, tenantID, id, from, to); err != nil {
		return err
	}
	if to == domain.BatchRejected || to == domain.BatchCancelled {
		if _, err := tx.ExecContext(ctx, `
			UPDATE lnm_contract_reservations r SET status = 'RELEASED'
			WHERE r.tenant_id = $1 AND r.source_type = 'DISBURSEMENT' AND r.status = 'HELD'
			  AND r.source_id IN (SELECT d.id FROM lnm_disbursements d
			                      WHERE d.tenant_id = $1 AND d.batch_id = $2)`, tenantID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func setDisbursementBatchStatus(ctx context.Context, q repoTX, tenantID, id, from, to string) error {
	expected := domain.DataVersionFromContext(ctx)
	res, err := q.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches SET status = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = $3
		  AND ($5 = 0 OR version = $5)`, tenantID, id, from, to, expected)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if staleVersion(ctx, q, "lnm_disbursement_batches", tenantID, id, expected) {
			return ErrStaleVersion
		}
		var current string
		lookupErr := q.QueryRowContext(ctx, `SELECT status FROM lnm_disbursement_batches WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&current)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("%w", ErrNotFound)
		}
		if lookupErr != nil {
			return lookupErr
		}
		return fmt.Errorf("%w: disbursement batch is %s, expected %s", domain.ErrInvalidTransition, current, from)
	}
	return nil
}

// SetDisbursementBatchPosted marks the batch POSTED with its journal entry.
func (r *LoanRepository) SetDisbursementBatchPosted(ctx context.Context, tenantID, id, journalEntryID string) error {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.Status(domain.BatchApproved), domain.Status(domain.BatchPosted), ""); err != nil {
		return err
	}
	expected := domain.DataVersionFromContext(ctx)
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches
		SET status = 'POSTED', journal_entry_id = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'APPROVED'
		  AND ($4 = 0 OR version = $4)`, tenantID, id, nullText(journalEntryID), expected)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if staleVersion(ctx, r.db, "lnm_disbursement_batches", tenantID, id, expected) {
			return ErrStaleVersion
		}
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}

// SumCompleteForAgreement totals the COMPLETE drawdowns already booked
// against one agreement — in-flight cases (PENDING_APPROVAL/APPROVED) count too, so
// concurrent batch completes cannot overshoot the register amounts
// (complete remainder guard, per agreement).
func (r *LoanRepository) SumCompleteForAgreement(ctx context.Context, tenantID, agreementCode string) (int64, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(disburse_amt_minor), 0)
		FROM lnm_disbursements
		WHERE tenant_id = $1 AND agreement_code = $2 AND flow_type = 'COMPLETE'
		  AND status IN ('PENDING_APPROVAL', 'APPROVED', 'POSTED')`, tenantID, agreementCode)
	var total int64
	return total, row.Scan(&total)
}

// SumPostedRegisterForAgreement totals the POSTED REGISTER drawdowns of one
// agreement — the ceiling a COMPLETE batch may not exceed.
func (r *LoanRepository) SumPostedRegisterForAgreement(ctx context.Context, tenantID, agreementCode string) (int64, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(disburse_amt_minor), 0)
		FROM lnm_disbursements
		WHERE tenant_id = $1 AND agreement_code = $2 AND flow_type = 'REGISTER' AND status = 'POSTED'`,
		tenantID, agreementCode)
	var total int64
	return total, row.Scan(&total)
}
