package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
)

// TransactionRepository persists transactions and ledger entries.
type TransactionRepository struct {
	db *sql.DB
}

func NewTransactionRepository(db *sql.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

// ── Transactions ──

func (r *TransactionRepository) Create(ctx context.Context, txn *domain.Transaction) error {
	meta := "null"
	if txn.Metadata != nil {
		b, _ := json.Marshal(txn.Metadata)
		meta = string(b)
	}

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_transactions (tenant_id, idempotency_key, txn_type, txn_date, status, description, source_ref, created_by, approved_by, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, posted_at, created_at
	`, txn.TenantID, nullStr(txn.IdempotencyKey), txn.TxnType, txn.TxnDate,
		string(txn.Status), txn.Description, txn.SourceRef, txn.CreatedBy,
		nullStr(txn.ApprovedBy), meta)

	return row.Scan(&txn.ID, &txn.PostedAt, &txn.CreatedAt)
}

func (r *TransactionRepository) GetByID(ctx context.Context, id string) (*domain.Transaction, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, idempotency_key, txn_type, txn_date, posted_at, status, description, source_ref, created_by, approved_by, metadata, created_at
		FROM fin_transactions WHERE id = $1
	`, id)
	return scanTransaction(row)
}

func (r *TransactionRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transaction, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, idempotency_key, txn_type, txn_date, posted_at, status, description, source_ref, created_by, approved_by, metadata, created_at
		FROM fin_transactions WHERE idempotency_key = $1
	`, key)
	return scanTransaction(row)
}

func (r *TransactionRepository) List(ctx context.Context, tenantID string, status string, from, to time.Time, page, size int) ([]domain.Transaction, int, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	idx := 2

	if status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, status)
		idx++
	}
	if !from.IsZero() {
		where = append(where, fmt.Sprintf("posted_at >= $%d", idx))
		args = append(args, from)
		idx++
	}
	if !to.IsZero() {
		where = append(where, fmt.Sprintf("posted_at <= $%d", idx))
		args = append(args, to)
		idx++
	}

	wc := strings.Join(where, " AND ")

	var total int
	r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fin_transactions WHERE "+wc, args...).Scan(&total)

	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	offset := (page - 1) * size

	query := fmt.Sprintf(`
		SELECT id, tenant_id, idempotency_key, txn_type, txn_date, posted_at, status, description, source_ref, created_by, approved_by, metadata, created_at
		FROM fin_transactions WHERE %s ORDER BY posted_at DESC LIMIT $%d OFFSET $%d
	`, wc, idx, idx+1)
	allArgs := append(args, size, offset)

	rows, err := r.db.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var txns []domain.Transaction
	for rows.Next() {
		var t domain.Transaction
		if err := scanTransactionRow(rows, &t); err != nil {
			return nil, 0, err
		}
		txns = append(txns, t)
	}
	return txns, total, rows.Err()
}

func (r *TransactionRepository) UpdateStatus(ctx context.Context, id string, status domain.TransactionStatus, approvedBy string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE fin_transactions SET status = $1, approved_by = $2 WHERE id = $3
	`, status, approvedBy, id)
	return err
}

// ── Ledger Entries ──

func (r *TransactionRepository) InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO fin_ledger_entries (entry_id, transaction_id, account_id, entry_type, amount, currency, description, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		meta := "null"
		if e.Metadata != nil {
			b, _ := json.Marshal(e.Metadata)
			meta = string(b)
		}
		if _, err := stmt.ExecContext(ctx, e.EntryID, e.TransactionID, e.AccountID,
			string(e.EntryType), e.Amount, e.Currency, e.Description, meta); err != nil {
			return fmt.Errorf("insert ledger entry: %w", err)
		}
	}

	return tx.Commit()
}

func (r *TransactionRepository) GetEntriesByTransaction(ctx context.Context, txnID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, entry_id, transaction_id, account_id, entry_type, amount, currency, posted_at, description, metadata
		FROM fin_ledger_entries WHERE transaction_id = $1 ORDER BY entry_type
	`, txnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var meta sql.NullString
		if err := rows.Scan(&e.ID, &e.EntryID, &e.TransactionID, &e.AccountID,
			&e.EntryType, &e.Amount, &e.Currency, &e.PostedAt, &e.Description, &meta); err != nil {
			return nil, err
		}
		if meta.Valid {
			json.Unmarshal([]byte(meta.String), &e.Metadata)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ── Balance update (materialized) ──

func (r *TransactionRepository) UpdateBalance(ctx context.Context, accountID string, delta string, normalBalance domain.NormalBalance, entryType domain.EntryType) error {
	sign := "+"
	if (normalBalance == domain.NormalDebit && entryType == domain.EntryCredit) ||
		(normalBalance == domain.NormalCredit && entryType == domain.EntryDebit) {
		sign = "-"
	}

	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE fin_account_balances
		SET balance = balance %s $1::numeric, updated_at = now()
		WHERE account_id = $2
	`, sign), delta, accountID)
	return err
}

// ── Scanners ──

func scanTransaction(row *sql.Row) (*domain.Transaction, error) {
	var t domain.Transaction
	if err := scanTransactionRow(row, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func scanTransactionRow(scanner interface{ Scan(dest ...any) error }, t *domain.Transaction) error {
	var meta sql.NullString
	var idKey sql.NullString
	var approvedBy sql.NullString

	err := scanner.Scan(&t.ID, &t.TenantID, &idKey, &t.TxnType, &t.TxnDate,
		&t.PostedAt, &t.Status, &t.Description, &t.SourceRef, &t.CreatedBy,
		&approvedBy, &meta, &t.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return err
	}
	t.IdempotencyKey = idKey.String
	t.ApprovedBy = approvedBy.String
	if meta.Valid {
		json.Unmarshal([]byte(meta.String), &t.Metadata)
	}
	return nil
}

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

var _ = time.Time{}
