package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
)

// AccountRepository persists accounts and balances.
type AccountRepository struct {
	db *sql.DB
}

func NewAccountRepository(db *sql.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

func (r *AccountRepository) Create(ctx context.Context, a *domain.Account) (*domain.Account, error) {
	meta := "null"
	if a.Metadata != nil {
		b, _ := json.Marshal(a.Metadata)
		meta = string(b)
	}

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_accounts (tenant_id, code, name, type, normal_balance, currency, is_active, parent_id, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at, updated_at
	`, a.TenantID, a.Code, a.Name, string(a.Type), string(a.NormalBalance),
		a.Currency, a.IsActive, nullUUID(a.ParentID), meta)

	err := row.Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}

	// Init balance
	_, _ = r.db.ExecContext(ctx, `
		INSERT INTO fin_account_balances (account_id, balance, currency)
		VALUES ($1, 0, $2) ON CONFLICT DO NOTHING
	`, a.ID, a.Currency)

	return a, nil
}

func (r *AccountRepository) GetByID(ctx context.Context, id string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, type, normal_balance, currency, is_active, parent_id, metadata, created_at, updated_at
		FROM fin_accounts WHERE id = $1
	`, id)
	return scanAccount(row)
}

func (r *AccountRepository) GetByCode(ctx context.Context, code string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, type, normal_balance, currency, is_active, parent_id, metadata, created_at, updated_at
		FROM fin_accounts WHERE code = $1
	`, code)
	return scanAccount(row)
}

func (r *AccountRepository) List(ctx context.Context, tenantID string) ([]domain.Account, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, type, normal_balance, currency, is_active, parent_id, metadata, created_at, updated_at
		FROM fin_accounts WHERE tenant_id = $1 ORDER BY code
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []domain.Account
	for rows.Next() {
		var a domain.Account
		if err := scanAccountRow(rows, &a); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func (r *AccountRepository) GetBalance(ctx context.Context, accountID string) (*domain.AccountBalance, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT account_id, balance, updated_at FROM fin_account_balances WHERE account_id = $1
	`, accountID)
	var b domain.AccountBalance
	err := row.Scan(&b.AccountID, &b.Balance, &b.AsOf)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func scanAccount(row *sql.Row) (*domain.Account, error) {
	var a domain.Account
	if err := scanAccountRow(row, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func scanAccountRow(scanner interface{ Scan(dest ...any) error }, a *domain.Account) error {
	var meta sql.NullString
	var parentID sql.NullString

	err := scanner.Scan(&a.ID, &a.TenantID, &a.Code, &a.Name, &a.Type,
		&a.NormalBalance, &a.Currency, &a.IsActive, &parentID, &meta, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows
		}
		return fmt.Errorf("scan account: %w", err)
	}
	if parentID.Valid {
		a.ParentID = parentID.String
	}
	if meta.Valid {
		json.Unmarshal([]byte(meta.String), &a.Metadata)
	}
	return nil
}

func nullUUID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

var _ = strings.TrimSpace
