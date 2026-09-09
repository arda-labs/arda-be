package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// ── Batches (iteration 13: 1 hồ sơ — N hợp đồng) ──

const disbursementBatchColumns = `id::text, tenant_id, COALESCE(org_code,''), flow_type, COALESCE(source_batch_id::text,''),
	txn_date::text, COALESCE(payment_method,''), COALESCE(account_code,''), COALESCE(currency_code,''), total_amt_minor,
	COALESCE(description,''), trader, status, workflow_case_id::text, COALESCE(workflow_case_code,''),
	journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at`

func scanDisbursementBatch(s interface{ Scan(...any) error }) (domain.DisbursementBatch, error) {
	var b domain.DisbursementBatch
	var caseID, entryID sql.NullString
	var trader []byte
	err := s.Scan(&b.ID, &b.TenantID, &b.OrgCode, &b.FlowType, &b.SourceBatchID,
		&b.TxnDate, &b.PaymentMethod, &b.AccountCode, &b.CurrencyCode, &b.TotalAmtMinor,
		&b.Description, &trader, &b.Status, &caseID, &b.WorkflowCaseCode,
		&entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt)
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

const collectionBatchColumns = `id::text, tenant_id, COALESCE(org_code,''), txn_date::text,
	COALESCE(payment_method,''), COALESCE(account_code,''), COALESCE(currency_code,''),
	total_principal_minor, total_interest_minor, COALESCE(description,''), trader, status,
	workflow_case_id::text, COALESCE(workflow_case_code,''), journal_entry_id::text,
	COALESCE(created_by,''), created_at, updated_at`

func scanCollectionBatch(s interface{ Scan(...any) error }) (domain.CollectionBatch, error) {
	var b domain.CollectionBatch
	var caseID, entryID sql.NullString
	var trader []byte
	err := s.Scan(&b.ID, &b.TenantID, &b.OrgCode, &b.TxnDate,
		&b.PaymentMethod, &b.AccountCode, &b.CurrencyCode,
		&b.TotalPrincipalMinor, &b.TotalInterestMinor, &b.Description, &trader, &b.Status,
		&caseID, &b.WorkflowCaseCode, &entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt)
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
			(id, tenant_id, org_code, flow_type, source_batch_id, txn_date, payment_method, account_code,
			 currency_code, total_amt_minor, description, trader, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6::date,$7,$8,$9,$10,$11,$12::jsonb,$13,$14)
		RETURNING created_at, updated_at`,
		b.ID, b.TenantID, b.OrgCode, b.FlowType, sourceID, b.TxnDate, b.PaymentMethod, b.AccountCode,
		b.CurrencyCode, b.TotalAmtMinor, b.Description, string(trader), b.Status, b.CreatedBy).
		Scan(&b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return err
	}
	for i := range b.Rows {
		row := &b.Rows[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO lnm_disbursements
				(tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor,
				 currency_code, flow_type, source_register_id, batch_id, is_closed, status, org_code, created_by)
			VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,'DRAFT',$11,$12)`,
			row.TenantID, row.ContractCode, row.AgreementCode, row.DisburseDate, row.DisburseAmtMinor,
			row.CurrencyCode, row.FlowType, nullText(row.SourceRegisterID), row.BatchID, row.IsClosed,
			row.OrgCode, row.CreatedBy); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateCollectionBatch inserts the batch header and its collection rows in
// one transaction.
func (r *LoanRepository) CreateCollectionBatch(ctx context.Context, b *domain.CollectionBatch) error {
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
	err = tx.QueryRowContext(ctx, `
		INSERT INTO lnm_collection_batches
			(id, tenant_id, org_code, txn_date, payment_method, account_code, currency_code,
			 total_principal_minor, total_interest_minor, description, trader, status, created_by)
		VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13)
		RETURNING created_at, updated_at`,
		b.ID, b.TenantID, b.OrgCode, b.TxnDate, b.PaymentMethod, b.AccountCode, b.CurrencyCode,
		b.TotalPrincipalMinor, b.TotalInterestMinor, b.Description, string(trader), b.Status, b.CreatedBy).
		Scan(&b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return err
	}
	for i := range b.Rows {
		row := &b.Rows[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO lnm_collections
				(tenant_id, contract_code, agreement_code, collection_date, principal_minor,
				 interest_minor, overdue_interest_minor, currency_code, batch_id, status, org_code, created_by)
			VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8,$9,'DRAFT',$10,$11)`,
			row.TenantID, row.ContractCode, row.AgreementCode, row.CollectionDate, row.PrincipalMinor,
			row.InterestMinor, row.OverdueInterestMinor, row.CurrencyCode, row.BatchID,
			row.OrgCode, row.CreatedBy); err != nil {
			return err
		}
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
		&entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &total)
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

// ListCollectionBatches is the paged collection batch list.
func (r *LoanRepository) ListCollectionBatches(ctx context.Context, tenantID, status string, limit, offset int) ([]domain.CollectionBatch, int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+collectionBatchColumns+`, count(*) OVER() AS total_count
		FROM lnm_collection_batches
		WHERE tenant_id = $1
		  AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC, id
		LIMIT $3 OFFSET $4`, tenantID, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []domain.CollectionBatch{}
	total := 0
	for rows.Next() {
		var b domain.CollectionBatch
		var caseID, entryID sql.NullString
		var trader []byte
		if err := rows.Scan(&b.ID, &b.TenantID, &b.OrgCode, &b.TxnDate,
			&b.PaymentMethod, &b.AccountCode, &b.CurrencyCode,
			&b.TotalPrincipalMinor, &b.TotalInterestMinor, &b.Description, &trader, &b.Status,
			&caseID, &b.WorkflowCaseCode, &entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &total); err != nil {
			return nil, 0, err
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
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
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

// GetCollectionBatch loads one collection batch header.
func (r *LoanRepository) GetCollectionBatch(ctx context.Context, tenantID, id string) (*domain.CollectionBatch, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+collectionBatchColumns+`
		FROM lnm_collection_batches WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	b, err := scanCollectionBatch(row)
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

// GetCollectionBatchRows returns the collection rows of one batch.
func (r *LoanRepository) GetCollectionBatchRows(ctx context.Context, tenantID, batchID string) ([]domain.Collection, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, contract_code, agreement_code, collection_date::text,
		       principal_minor, interest_minor, overdue_interest_minor, currency_code, COALESCE(batch_id::text,''),
		       status, workflow_case_id::text, COALESCE(workflow_case_code,''), journal_entry_id::text,
		       created_by, created_at, updated_at
		FROM lnm_collections
		WHERE tenant_id = $1 AND batch_id = $2
		ORDER BY created_at, id`, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Collection{}
	for rows.Next() {
		var c domain.Collection
		var caseID, entryID sql.NullString
		if err := rows.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.AgreementCode, &c.CollectionDate,
			&c.PrincipalMinor, &c.InterestMinor, &c.OverdueInterestMinor, &c.CurrencyCode, &c.BatchID,
			&c.Status, &caseID, &c.WorkflowCaseCode, &entryID,
			&c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if caseID.Valid {
			c.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			c.JournalEntryID = &entryID.String
		}
		out = append(out, c)
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
func (r *LoanRepository) SetDisbursementBatchStatus(ctx context.Context, tenantID, id, status string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches SET status = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}

// SetDisbursementBatchPosted marks the batch POSTED with its journal entry.
func (r *LoanRepository) SetDisbursementBatchPosted(ctx context.Context, tenantID, id, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_disbursement_batches
		SET status = 'POSTED', journal_entry_id = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nullText(journalEntryID))
	return err
}

// SetCollectionBatchCase records the workflow case on the collection batch.
func (r *LoanRepository) SetCollectionBatchCase(ctx context.Context, tenantID, id, caseID, caseCode string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_collection_batches
		SET workflow_case_id = $3, workflow_case_code = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nullText(caseID), caseCode)
	return err
}

// SetCollectionBatchStatus transitions the collection batch status.
func (r *LoanRepository) SetCollectionBatchStatus(ctx context.Context, tenantID, id, status string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_collection_batches SET status = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}

// SetCollectionBatchPosted marks the collection batch POSTED with its journal entry.
func (r *LoanRepository) SetCollectionBatchPosted(ctx context.Context, tenantID, id, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_collection_batches
		SET status = 'POSTED', journal_entry_id = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nullText(journalEntryID))
	return err
}

// SumOutstandingAndPendingByContract totals one contract's settled
// outstanding plus in-transit pending across its agreements — the consumed
// headroom for the batch register guard (both SUMs in one round trip).
func (r *LoanRepository) SumOutstandingAndPendingByContract(ctx context.Context, tenantID, contractCode string) (outstanding, pending int64, err error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(outstanding_amt_minor), 0), COALESCE(SUM(COALESCE(pending_disburse_amt_minor, 0)), 0)
		FROM lnm_agreements WHERE tenant_id = $1 AND contract_code = $2`, tenantID, contractCode)
	return outstanding, pending, row.Scan(&outstanding, &pending)
}

// SumCompleteForAgreement totals the COMPLETE drawdowns already booked
// against one agreement — in-flight cases (SUBMITTED/APPROVED) count too, so
// concurrent batch completes cannot overshoot the register amounts
// (complete remainder guard, per agreement).
func (r *LoanRepository) SumCompleteForAgreement(ctx context.Context, tenantID, agreementCode string) (int64, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(disburse_amt_minor), 0)
		FROM lnm_disbursements
		WHERE tenant_id = $1 AND agreement_code = $2 AND flow_type = 'COMPLETE'
		  AND status IN ('SUBMITTED', 'APPROVED', 'POSTED')`, tenantID, agreementCode)
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

// CloseContractByCode statuses a contract CLOSED (batch COMPLETE settle of
// an is_closed row — mirror of the single-row write-off close semantics).
func (r *LoanRepository) CloseContractByCode(ctx context.Context, tenantID, contractCode string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_contracts SET status = 'CLOSED', updated_at = now()
		WHERE tenant_id = $1 AND contract_code = $2`, tenantID, contractCode)
	return err
}
