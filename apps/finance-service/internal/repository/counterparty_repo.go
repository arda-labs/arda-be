package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Counterparty is one partner master row (W4c-E).
type Counterparty struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	PartyType string    `json:"party_type"`
	OrgCode   string    `json:"org_code,omitempty"`
	Note      string    `json:"note,omitempty"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CounterpartyAccount is one partner bank/GL account row.
type CounterpartyAccount struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	CounterpartyID string    `json:"counterparty_id"`
	AccountNo      string    `json:"account_no"`
	BankCode       string    `json:"bank_code,omitempty"`
	CoaAccountCode string    `json:"coa_account_code,omitempty"`
	CurrencyCode   string    `json:"currency_code"`
	IsDefault      bool      `json:"is_default"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ListCounterparties returns partner rows filtered by q/type.
func (r *ConfigRepository) ListCounterparties(ctx context.Context, tenantID, q, partyType string, includeInactive bool) ([]Counterparty, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if !includeInactive {
		where = append(where, "is_active")
	}
	if q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", len(args), len(args)))
	}
	if partyType != "" {
		args = append(args, partyType)
		where = append(where, fmt.Sprintf("party_type = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, code, name, party_type, COALESCE(org_code,''), COALESCE(note,''),
		       is_active, COALESCE(created_by,''), created_at, updated_at
		FROM fin_counterparties WHERE `+strings.Join(where, " AND ")+`
		ORDER BY code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Counterparty{}
	for rows.Next() {
		var x Counterparty
		if err := rows.Scan(&x.ID, &x.TenantID, &x.Code, &x.Name, &x.PartyType, &x.OrgCode, &x.Note,
			&x.IsActive, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// UpsertCounterparty inserts or updates by (tenant, code).
func (r *ConfigRepository) UpsertCounterparty(ctx context.Context, in *Counterparty) (*Counterparty, error) {
	if in.PartyType == "" {
		in.PartyType = "OTHER"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_counterparties (tenant_id, code, name, party_type, org_code, note, is_active, created_by)
		VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,COALESCE($7,true),$8)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name,
			party_type = EXCLUDED.party_type, org_code = EXCLUDED.org_code, note = EXCLUDED.note,
			is_active = EXCLUDED.is_active, updated_at = now(), version = fin_counterparties.version + 1
		RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.Code, in.Name, in.PartyType, in.OrgCode, in.Note, in.IsActive, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

// SetCounterpartyActive toggles the soft-delete flag by id.
func (r *ConfigRepository) SetCounterpartyActive(ctx context.Context, tenantID, id string, active bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE fin_counterparties SET is_active = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id, active)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("counterparty not found")
	}
	return nil
}

// UpdateCounterparty updates the editable fields by id.
func (r *ConfigRepository) UpdateCounterparty(ctx context.Context, tenantID, id, actor string, in *Counterparty) (*Counterparty, error) {
	if in.PartyType == "" {
		in.PartyType = "OTHER"
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE fin_counterparties SET name = $3, party_type = $4, org_code = NULLIF($5,''), note = $6,
			is_active = COALESCE($7,true), updated_by = $8, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid
		RETURNING id::text, code, created_at, updated_at`,
		tenantID, id, in.Name, in.PartyType, in.OrgCode, in.Note, in.IsActive, actor)
	if err := row.Scan(&in.ID, &in.Code, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	in.TenantID = tenantID
	return in, nil
}

// ListCounterpartyAccounts returns the accounts of one partner.
func (r *ConfigRepository) ListCounterpartyAccounts(ctx context.Context, tenantID, counterpartyID string) ([]CounterpartyAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, counterparty_id::text, account_no, bank_code, coa_account_code,
		       currency_code, is_default, created_at, updated_at
		FROM fin_counterparty_accounts WHERE tenant_id = $1 AND counterparty_id = $2::uuid
		ORDER BY is_default DESC, account_no`, tenantID, counterpartyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CounterpartyAccount{}
	for rows.Next() {
		var x CounterpartyAccount
		if err := rows.Scan(&x.ID, &x.CounterpartyID, &x.AccountNo, &x.BankCode, &x.CoaAccountCode,
			&x.CurrencyCode, &x.IsDefault, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// UpsertCounterpartyAccount inserts or updates one partner account.
func (r *ConfigRepository) UpsertCounterpartyAccount(ctx context.Context, in *CounterpartyAccount) (*CounterpartyAccount, error) {
	if in.CurrencyCode == "" {
		in.CurrencyCode = "VND"
	}
	if in.IsDefault {
		if _, err := r.db.ExecContext(ctx, `
			UPDATE fin_counterparty_accounts SET is_default = false, updated_at = now()
			WHERE tenant_id = $1 AND counterparty_id = $2::uuid`, in.TenantID, in.CounterpartyID); err != nil {
			return nil, err
		}
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO fin_counterparty_accounts (tenant_id, counterparty_id, account_no, bank_code,
			coa_account_code, currency_code, is_default)
		VALUES ($1,$2::uuid,$3,$4,$5,$6,$7)
		ON CONFLICT (tenant_id, counterparty_id, account_no) DO UPDATE SET
			bank_code = EXCLUDED.bank_code, coa_account_code = EXCLUDED.coa_account_code,
			currency_code = EXCLUDED.currency_code, is_default = EXCLUDED.is_default, updated_at = now()
		RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.CounterpartyID, in.AccountNo, in.BankCode, in.CoaAccountCode, in.CurrencyCode, in.IsDefault)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

var _ = sql.ErrNoRows
