package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

const collectionBatchColumns = `id::text, tenant_id, COALESCE(org_code,''), txn_date::text,
	COALESCE(payment_method,''), COALESCE(account_code,''), COALESCE(currency_code,''),
	total_principal_minor, total_interest_minor, COALESCE(description,''), trader, status,
	workflow_case_id::text, COALESCE(workflow_case_code,''), journal_entry_id::text,
	COALESCE(created_by,''), created_at, updated_at, version`

func scanCollectionBatch(s interface{ Scan(...any) error }) (domain.CollectionBatch, error) {
	var b domain.CollectionBatch
	var caseID, entryID sql.NullString
	var trader []byte
	err := s.Scan(&b.ID, &b.TenantID, &b.OrgCode, &b.TxnDate,
		&b.PaymentMethod, &b.AccountCode, &b.CurrencyCode,
		&b.TotalPrincipalMinor, &b.TotalInterestMinor, &b.Description, &trader, &b.Status,
		&caseID, &b.WorkflowCaseCode, &entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.DataVersion)
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
			&caseID, &b.WorkflowCaseCode, &entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.DataVersion, &total); err != nil {
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

// SetCollectionBatchCase records the workflow case on the collection batch.
func (r *LoanRepository) SetCollectionBatchCase(ctx context.Context, tenantID, id, caseID, caseCode string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE lnm_collection_batches
		SET workflow_case_id = $3, workflow_case_code = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nullText(caseID), caseCode)
	return err
}

// SetCollectionBatchStatus transitions the collection batch status.
func (r *LoanRepository) SetCollectionBatchStatus(ctx context.Context, tenantID, id, from, to, reason string) error {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.Status(from), domain.Status(to), reason); err != nil {
		return err
	}
	if to == domain.BatchRejected || to == domain.BatchCancelled {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := setCollectionBatchStatus(ctx, tx, tenantID, id, from, to); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE lnm_collections SET status = $4, updated_at = now(), version = version + 1
			WHERE tenant_id = $1 AND batch_id = $2 AND status = $3`, tenantID, id, from, to); err != nil {
			return err
		}
		return tx.Commit()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setCollectionBatchStatus(ctx, tx, tenantID, id, from, to); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE lnm_collections SET status = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND batch_id = $2 AND status = $3`, tenantID, id, from, to); err != nil {
		return err
	}
	return tx.Commit()
}

func setCollectionBatchStatus(ctx context.Context, q repoTX, tenantID, id, from, to string) error {
	expected := domain.DataVersionFromContext(ctx)
	res, err := q.ExecContext(ctx, `
		UPDATE lnm_collection_batches SET status = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = $3
		  AND ($5 = 0 OR version = $5)`, tenantID, id, from, to, expected)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if staleVersion(ctx, q, "lnm_collection_batches", tenantID, id, expected) {
			return ErrStaleVersion
		}
		var current string
		lookupErr := q.QueryRowContext(ctx, `SELECT status FROM lnm_collection_batches WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&current)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("%w", ErrNotFound)
		}
		if lookupErr != nil {
			return lookupErr
		}
		return fmt.Errorf("%w: collection batch is %s, expected %s", domain.ErrInvalidTransition, current, from)
	}
	return nil
}

// SetCollectionBatchPosted marks the collection batch POSTED with its journal entry.
func (r *LoanRepository) SetCollectionBatchPosted(ctx context.Context, tenantID, id, journalEntryID string) error {
	if err := domain.CanTransition(domain.WorkflowMachine, domain.Status(domain.BatchApproved), domain.Status(domain.BatchPosted), ""); err != nil {
		return err
	}
	expected := domain.DataVersionFromContext(ctx)
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_collection_batches
		SET status = 'POSTED', journal_entry_id = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'APPROVED'
		  AND ($4 = 0 OR version = $4)`, tenantID, id, nullText(journalEntryID), expected)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if staleVersion(ctx, r.db, "lnm_collection_batches", tenantID, id, expected) {
			return ErrStaleVersion
		}
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}
