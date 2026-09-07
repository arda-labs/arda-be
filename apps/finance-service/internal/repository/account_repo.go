package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
)

// AccountRepository persists the account master.
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


	return a, nil
}

func (r *AccountRepository) GetByID(ctx context.Context, tenantID, id string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, type, normal_balance, currency, is_active, parent_id, metadata, created_at, updated_at
		FROM fin_accounts WHERE tenant_id = $1 AND id = $2
	`, tenantID, id)
	return scanAccount(row)
}

func (r *AccountRepository) GetByCode(ctx context.Context, tenantID, code string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, type, normal_balance, currency, is_active, parent_id, metadata, created_at, updated_at
		FROM fin_accounts WHERE tenant_id = $1 AND code = $2
	`, tenantID, code)
	return scanAccount(row)
}

func (r *AccountRepository) GetByTenantCode(ctx context.Context, tenantID, code string) (*domain.Account, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, type, normal_balance, currency, is_active, parent_id, metadata, created_at, updated_at
		FROM fin_accounts WHERE tenant_id = $1 AND code = $2
	`, tenantID, code)
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

// ListAccountsParams carries the paged account-list contract: q ILIKEs
// code+name, sort is a whitelist key, paging is SQL LIMIT/OFFSET (mirrors
// iam-service ListGroupsParams).
type ListAccountsParams struct {
	Page     int
	Size     int
	TenantID string
	Search   string
	Sort     string
	Order    string
}

// accountSortCol maps the FE sort param to a whitelisted column; unknown
// values fall back to code (the previous fixed ordering).
func accountSortCol(sort string) string {
	switch sort {
	case "name":
		return "name"
	case "created_at":
		return "created_at"
	default:
		return "code"
	}
}

func listSortDirection(order string) string {
	if order == "desc" {
		return "DESC"
	}
	return "ASC"
}

// ListPaged returns one page of accounts plus the unfiltered total. The
// handler passes Search/Sort/Order straight from the parsed list request —
// they are interpolated only through the whitelist helpers above.
func (r *AccountRepository) ListPaged(ctx context.Context, params ListAccountsParams) ([]domain.Account, int, error) {
	where := []string{"tenant_id = $1"}
	args := []any{params.TenantID}
	if params.Search != "" {
		args = append(args, "%"+params.Search+"%")
		where = append(where, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", len(args), len(args)))
	}

	wc := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM fin_accounts WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count accounts: %w", err)
	}

	page := params.Page
	if page < 1 {
		page = 1
	}
	size := params.Size
	if size < 1 {
		size = 20
	}
	query := fmt.Sprintf(`
		SELECT id, tenant_id, code, name, type, normal_balance, currency, is_active, parent_id, metadata, created_at, updated_at
		FROM fin_accounts WHERE %s
		ORDER BY %s %s
		LIMIT %d OFFSET %d
	`, wc, accountSortCol(params.Sort), listSortDirection(params.Order), size, (page-1)*size)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	accounts := []domain.Account{}
	for rows.Next() {
		var a domain.Account
		if err := scanAccountRow(rows, &a); err != nil {
			return nil, 0, err
		}
		accounts = append(accounts, a)
	}
	return accounts, total, rows.Err()
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
