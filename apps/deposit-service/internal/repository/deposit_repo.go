package repository

import (
	"context"
	cryptoRand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// SavingsProduct is a deposit product catalog row (P2.1).
type SavingsProduct struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	TermMonths   int       `json:"term_months"`
	InterestRate float64   `json:"interest_rate"`
	CurrencyCode string    `json:"currency_code"`
	IsActive     bool      `json:"is_active"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Savings is one citizen deposit account (EPAS dpm_inf_saving subset).
type Savings struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	SavingsCode    string    `json:"savings_code"`
	CustomerCode   string    `json:"customer_code"`
	ProductCode    string    `json:"product_code"`
	OpenDate       string    `json:"open_date"`
	MaturityDate   string    `json:"maturity_date"`
	PrincipalMinor int64     `json:"principal_minor"`
	AccruedMinor   int64     `json:"accrued_minor"`
	CurrencyCode   string    `json:"currency_code"`
	OrgCode        string    `json:"org_code,omitempty"`
	Status         string    `json:"status"`
	WorkflowCaseID *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID *string   `json:"journal_entry_id,omitempty"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// DepositTxn is one movement on a savings account.
type DepositTxn struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	SavingsID      string    `json:"savings_id"`
	TxnType        string    `json:"txn_type"`
	AmountMinor    int64     `json:"amount_minor"`
	CurrencyCode   string    `json:"currency_code"`
	TxnDate        string    `json:"txn_date"`
	Status         string    `json:"status"`
	WorkflowCaseID *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID *string   `json:"journal_entry_id,omitempty"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
}

// InterbankDeposit is one IBM deposit contract (EPAS ibm_inf_dep_contract subset).
type InterbankDeposit struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	DepositCode      string    `json:"deposit_code"`
	CounterpartyCode string    `json:"counterparty_code"`
	DepositDate      string    `json:"deposit_date"`
	MaturityDate     string    `json:"maturity_date"`
	PrincipalMinor   int64     `json:"principal_minor"`
	InterestRate     float64   `json:"interest_rate"`
	AccruedMinor     int64     `json:"accrued_minor"`
	CurrencyCode     string    `json:"currency_code"`
	OrgCode          string    `json:"org_code,omitempty"`
	Status           string    `json:"status"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// DepositRepository persists deposit domain data.
type DepositRepository struct {
	db *sql.DB
}

func NewDepositRepository(db *sql.DB) *DepositRepository {
	return &DepositRepository{db: db}
}

// ListProducts returns active products.
func (r *DepositRepository) ListProducts(ctx context.Context, tenantID string) ([]SavingsProduct, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, term_months, interest_rate, currency_code, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM dpm_products WHERE tenant_id = $1 AND is_active ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavingsProduct{}
	for rows.Next() {
		var p SavingsProduct
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.TermMonths, &p.InterestRate,
			&p.CurrencyCode, &p.IsActive, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertProduct creates or updates a product.
func (r *DepositRepository) UpsertProduct(ctx context.Context, p *SavingsProduct) (*SavingsProduct, error) {
	if p.ID == "" {
		p.ID = NewDepositID("dpmprd")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dpm_products (id, tenant_id, code, name, term_months, interest_rate, currency_code, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,true,$8)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name, term_months = EXCLUDED.term_months,
			interest_rate = EXCLUDED.interest_rate, updated_at = now(), version = dpm_products.version + 1
		RETURNING created_at, updated_at`,
		p.ID, p.TenantID, p.Code, p.Name, p.TermMonths, p.InterestRate, p.CurrencyCode, p.CreatedBy)
	if err := row.Scan(&p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ListSavings returns savings accounts filtered by status.
func (r *DepositRepository) ListSavings(ctx context.Context, tenantID string, orgCodes []string, status, q string) ([]Savings, error) {
	orgAny := any(nil)
	if len(orgCodes) > 0 {
		orgAny = orgCodes
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, savings_code, customer_code, product_code, open_date::text, maturity_date::text,
		       principal_minor, accrued_minor, currency_code, COALESCE(org_code,''), status,
		       workflow_case_id::text, journal_entry_id::text, created_by, created_at, updated_at
		FROM dpm_savings
		WHERE tenant_id = $1
		  AND ($4::text = '' OR status = $4::text)
		  AND ($5::text = '' OR savings_code ILIKE '%'||$5::text||'%' OR customer_code ILIKE '%'||$5::text||'%')
		  AND ($6::text[] IS NULL OR org_code = ANY($6::text[]))
		ORDER BY open_date DESC LIMIT 200`, tenantID, orgAny, status, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Savings{}
	for rows.Next() {
		var s Savings
		var caseID, entryID sql.NullString
		if err := rows.Scan(&s.ID, &s.TenantID, &s.SavingsCode, &s.CustomerCode, &s.ProductCode,
			&s.OpenDate, &s.MaturityDate, &s.PrincipalMinor, &s.AccruedMinor, &s.CurrencyCode,
			&s.OrgCode, &s.Status, &caseID, &entryID, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if caseID.Valid {
			s.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			s.JournalEntryID = &entryID.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSavingsByCode loads one savings account by business code.
func (r *DepositRepository) GetSavingsByCode(ctx context.Context, tenantID, code string) (*Savings, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, savings_code, customer_code, product_code, open_date::text, maturity_date::text,
		       principal_minor, accrued_minor, currency_code, COALESCE(org_code,''), status,
		       workflow_case_id::text, journal_entry_id::text, created_by, created_at, updated_at
		FROM dpm_savings WHERE tenant_id = $1 AND savings_code = $2`, tenantID, code)
	var s Savings
	var caseID, entryID sql.NullString
	err := row.Scan(&s.ID, &s.TenantID, &s.SavingsCode, &s.CustomerCode, &s.ProductCode,
		&s.OpenDate, &s.MaturityDate, &s.PrincipalMinor, &s.AccruedMinor, &s.CurrencyCode,
		&s.OrgCode, &s.Status, &caseID, &entryID, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("savings %s not found", code)
	}
	if err != nil {
		return nil, err
	}
	if caseID.Valid {
		s.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		s.JournalEntryID = &entryID.String
	}
	return &s, nil
}

// CreateSavings inserts a new savings account.
func (r *DepositRepository) CreateSavings(ctx context.Context, s *Savings) (*Savings, error) {
	if s.ID == "" {
		s.ID = NewDepositID("sv")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dpm_savings (id, tenant_id, savings_code, customer_code, product_code, open_date,
			maturity_date, principal_minor, accrued_minor, currency_code, org_code, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,0,$9,$10,'ACTIVE',$11)
		RETURNING created_at, updated_at`,
		s.ID, s.TenantID, s.SavingsCode, s.CustomerCode, s.ProductCode, s.OpenDate,
		s.MaturityDate, s.PrincipalMinor, s.CurrencyCode, nullStringDep(s.OrgCode), s.CreatedBy)
	if err := row.Scan(&s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return s, nil
}

// RecordTxn inserts one transaction row.
func (r *DepositRepository) RecordTxn(ctx context.Context, t *DepositTxn) (*DepositTxn, error) {
	if t.ID == "" {
		t.ID = NewDepositID("dpmtx")
	}
	status := t.Status
	if status == "" {
		status = "DRAFT"
	}
	var entryID any
	if t.JournalEntryID != nil && *t.JournalEntryID != "" {
		entryID = *t.JournalEntryID
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dpm_transactions (id, tenant_id, savings_id, txn_type, amount_minor, currency_code, txn_date, status, journal_entry_id, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING created_at`,
		t.ID, t.TenantID, t.SavingsID, t.TxnType, t.AmountMinor, t.CurrencyCode, t.TxnDate, status, entryID, t.CreatedBy)
	if err := row.Scan(&t.CreatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

// SetTxnJournal stamps the journal entry id + status on a transaction.
func (r *DepositRepository) SetTxnJournal(ctx context.Context, tenantID, id, status, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_transactions SET status = $3, journal_entry_id = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status, nullStringDep(journalEntryID))
	return err
}

// CloseSavings marks the account CLOSED after full settlement.
func (r *DepositRepository) CloseSavings(ctx context.Context, tenantID, savingsID, journalEntryID, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_savings SET status = 'CLOSED', journal_entry_id = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE'`, tenantID, savingsID, journalEntryID, actor)
	return err
}

// ApplyTopUp increases principal; ApplyWithdraw decreases.
func (r *DepositRepository) ApplyTopUp(ctx context.Context, tenantID, savingsID string, amountMinor int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_savings SET principal_minor = principal_minor + $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE'`, tenantID, savingsID, amountMinor)
	return err
}

func (r *DepositRepository) ApplyWithdraw(ctx context.Context, tenantID, savingsID string, amountMinor int64) error {
	tag, err := r.db.ExecContext(ctx, `
		UPDATE dpm_savings SET principal_minor = GREATEST(principal_minor - $3, 0), updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE' AND principal_minor >= $3`,
		tenantID, savingsID, amountMinor)
	if err != nil {
		return err
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return fmt.Errorf("insufficient principal or account not active")
	}
	return nil
}

// ListInterbankDeposits returns IBM contracts.
func (r *DepositRepository) ListInterbankDeposits(ctx context.Context, tenantID string, orgCodes []string, status string) ([]InterbankDeposit, error) {
	orgAny := any(nil)
	if len(orgCodes) > 0 {
		orgAny = orgCodes
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, deposit_code, counterparty_code, deposit_date::text, maturity_date::text,
		       principal_minor, interest_rate, accrued_minor, currency_code, COALESCE(org_code,''), status,
		       COALESCE(created_by,''), created_at, updated_at
		FROM ibm_deposits
		WHERE tenant_id = $1
		  AND ($4::text = '' OR status = $4::text)
		  AND ($5::text[] IS NULL OR org_code = ANY($5::text[]))
		ORDER BY deposit_date DESC LIMIT 200`, tenantID, orgAny, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterbankDeposit{}
	for rows.Next() {
		var d InterbankDeposit
		if err := rows.Scan(&d.ID, &d.TenantID, &d.DepositCode, &d.CounterpartyCode, &d.DepositDate,
			&d.MaturityDate, &d.PrincipalMinor, &d.InterestRate, &d.AccruedMinor, &d.CurrencyCode,
			&d.OrgCode, &d.Status, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CreateInterbankDeposit inserts an IBM contract.
func (r *DepositRepository) CreateInterbankDeposit(ctx context.Context, d *InterbankDeposit) (*InterbankDeposit, error) {
	if d.ID == "" {
		d.ID = NewDepositID("ibm")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO ibm_deposits (id, tenant_id, deposit_code, counterparty_code, deposit_date, maturity_date,
			principal_minor, interest_rate, accrued_minor, currency_code, org_code, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,0,$8,$9,$10,'ACTIVE',$11)
		RETURNING created_at, updated_at`,
		d.ID, d.TenantID, d.DepositCode, d.CounterpartyCode, d.DepositDate, d.MaturityDate,
		d.PrincipalMinor, d.InterestRate, d.CurrencyCode, nullStringDep(d.OrgCode), d.CreatedBy)
	if err := row.Scan(&d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return d, nil
}

// NewDepositID generates a prefixed random id.
func NewDepositID(prefix string) string {
	var b [16]byte
	if _, err := cryptoRand.Read(b[:]); err != nil {
		panic("deposit id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func nullStringDep(s string) any {
	if s == "" {
		return nil
	}
	return s
}
