package repository

import (
	"context"
	cryptoRand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
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
	ProductCode      string    `json:"product_code,omitempty"`
	CounterpartyCode string    `json:"counterparty_code"`
	CounterpartyName string    `json:"counterparty_name,omitempty"`
	DepositDate      string    `json:"deposit_date"`
	MaturityDate     string    `json:"maturity_date"`
	PrincipalMinor   int64     `json:"principal_minor"`
	InterestRate     float64   `json:"interest_rate"`
	AccruedMinor     int64     `json:"accrued_minor"`
	LastInterestDate string    `json:"last_interest_date,omitempty"`
	CurrencyCode     string    `json:"currency_code"`
	OrgCode          string    `json:"org_code,omitempty"`
	Status           string    `json:"status"`
	WorkflowCaseID   *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID   *string   `json:"journal_entry_id,omitempty"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// IBMProduct is one interbank deposit product catalog row.
type IBMProduct struct {
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

// IBMMovement is one staged IBM movement (EPAS IBM.300/301/302/304).
type IBMMovement struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	DepositID      string    `json:"deposit_id"`
	Kind           string    `json:"kind"`
	AmountMinor    int64     `json:"amount_minor"`
	CurrencyCode   string    `json:"currency_code"`
	MovementDate   string    `json:"movement_date"`
	PeriodFrom     string    `json:"period_from,omitempty"`
	PeriodTo       string    `json:"period_to,omitempty"`
	Note           string    `json:"note,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Status         string    `json:"status"`
	WorkflowCaseID *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID *string   `json:"journal_entry_id,omitempty"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// DepositRepository persists deposit domain data.
type DepositRepository struct {
	db *sql.DB
}

func NewDepositRepository(db *sql.DB) *DepositRepository {
	return &DepositRepository{db: db}
}

// ListProductsParams carries the parsed list query for the product catalog.
// The table is a small catalog, so the list stays unpaged; q narrows the set
// and sort is a repo-whitelisted column so export and table stay consistent.
type ListProductsParams struct {
	TenantID string
	Q        string
	IsActive *bool
	Sort     string
	Order    string
}

// productSortCol maps the FE sort param to a whitelisted column.
func productSortCol(sort string) string {
	switch sort {
	case "code":
		return "code"
	case "name":
		return "name"
	case "created_at":
		return "created_at"
	default:
		return "code"
	}
}

func listDepOrder(order string) string {
	if order == "desc" {
		return "DESC"
	}
	return "ASC"
}

// ListProducts returns active products filtered by q and is_active, sorted by
// the whitelisted column.
func (r *DepositRepository) ListProducts(ctx context.Context, params ListProductsParams) ([]SavingsProduct, error) {
	where := []string{"tenant_id = $1"}
	args := []any{params.TenantID}
	if params.Q != "" {
		args = append(args, "%"+params.Q+"%")
		where = append(where, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", len(args), len(args)))
	}
	if params.IsActive != nil {
		args = append(args, *params.IsActive)
		where = append(where, fmt.Sprintf("is_active = $%d", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, code, name, term_months, interest_rate, currency_code, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM dpm_products WHERE %s ORDER BY %s %s`,
		strings.Join(where, " AND "), productSortCol(params.Sort), listDepOrder(params.Order)), args...)
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

// ListSavings returns savings accounts filtered by status, org scope and q.
func (r *DepositRepository) ListSavings(ctx context.Context, tenantID string, orgCodes []string, status, q string) ([]Savings, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if len(orgCodes) > 0 {
		args = append(args, orgCodes)
		where = append(where, fmt.Sprintf("org_code = ANY($%d::text[])", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	if q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf(
			"(savings_code ILIKE $%d OR customer_code ILIKE $%d)", len(args), len(args)))
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, savings_code, customer_code, product_code, open_date::text, maturity_date::text,
		       principal_minor, accrued_minor, currency_code, COALESCE(org_code,''), status,
		       workflow_case_id::text, journal_entry_id::text, created_by, created_at, updated_at
		FROM dpm_savings
		WHERE %s
		ORDER BY open_date DESC LIMIT 200`, strings.Join(where, " AND ")), args...)
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
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, nullStringDep(journalEntryID))
	return err
}

// ListTxnsBySavings returns transactions of one savings account.
func (r *DepositRepository) ListTxnsBySavings(ctx context.Context, tenantID, savingsID string) ([]DepositTxn, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, savings_id, txn_type, amount_minor, currency_code, txn_date::text, status,
		       workflow_case_id::text, journal_entry_id::text, COALESCE(created_by,''), created_at
		FROM dpm_transactions WHERE tenant_id = $1 AND savings_id = $2 ORDER BY created_at DESC LIMIT 200`,
		tenantID, savingsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DepositTxn{}
	for rows.Next() {
		var t DepositTxn
		var caseID, entryID sql.NullString
		if err := rows.Scan(&t.ID, &t.TenantID, &t.SavingsID, &t.TxnType, &t.AmountMinor, &t.CurrencyCode,
			&t.TxnDate, &t.Status, &caseID, &entryID, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		if caseID.Valid {
			t.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			t.JournalEntryID = &entryID.String
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetProductByCode loads one savings product by code.
func (r *DepositRepository) GetProductByCode(ctx context.Context, tenantID, code string) (*SavingsProduct, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, term_months, interest_rate, currency_code, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM dpm_products WHERE tenant_id = $1 AND code = $2`, tenantID, code)
	var p SavingsProduct
	err := row.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.TermMonths, &p.InterestRate,
		&p.CurrencyCode, &p.IsActive, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
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

// ListInterbankDeposits returns IBM contracts filtered by status and org scope.
func (r *DepositRepository) ListInterbankDeposits(ctx context.Context, tenantID string, orgCodes []string, status string) ([]InterbankDeposit, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if len(orgCodes) > 0 {
		args = append(args, orgCodes)
		where = append(where, fmt.Sprintf("org_code = ANY($%d::text[])", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, deposit_code, COALESCE(product_code,''), counterparty_code,
		       COALESCE(counterparty_name,''), deposit_date::text, maturity_date::text,
		       principal_minor, interest_rate, accrued_minor, COALESCE(last_interest_date::text,''),
		       currency_code, COALESCE(org_code,''), status, workflow_case_id::text,
		       journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at
		FROM ibm_deposits
		WHERE %s
		ORDER BY deposit_date DESC LIMIT 200`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterbankDeposit{}
	for rows.Next() {
		var d InterbankDeposit
		var caseID, entryID sql.NullString
		if err := rows.Scan(&d.ID, &d.TenantID, &d.DepositCode, &d.ProductCode, &d.CounterpartyCode,
			&d.CounterpartyName, &d.DepositDate, &d.MaturityDate, &d.PrincipalMinor, &d.InterestRate,
			&d.AccruedMinor, &d.LastInterestDate, &d.CurrencyCode, &d.OrgCode, &d.Status,
			&caseID, &entryID, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if caseID.Valid {
			d.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			d.JournalEntryID = &entryID.String
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetInterbankDepositByID loads one IBM contract.
func (r *DepositRepository) GetInterbankDepositByID(ctx context.Context, tenantID, id string) (*InterbankDeposit, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, deposit_code, COALESCE(product_code,''), counterparty_code,
		       COALESCE(counterparty_name,''), deposit_date::text, maturity_date::text,
		       principal_minor, interest_rate, accrued_minor, COALESCE(last_interest_date::text,''),
		       currency_code, COALESCE(org_code,''), status, workflow_case_id::text,
		       journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at
		FROM ibm_deposits WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	var d InterbankDeposit
	var caseID, entryID sql.NullString
	err := row.Scan(&d.ID, &d.TenantID, &d.DepositCode, &d.ProductCode, &d.CounterpartyCode,
		&d.CounterpartyName, &d.DepositDate, &d.MaturityDate, &d.PrincipalMinor, &d.InterestRate,
		&d.AccruedMinor, &d.LastInterestDate, &d.CurrencyCode, &d.OrgCode, &d.Status,
		&caseID, &entryID, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if caseID.Valid {
		d.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		d.JournalEntryID = &entryID.String
	}
	return &d, nil
}

// CreateInterbankDeposit inserts an IBM contract with the supplied status.
func (r *DepositRepository) CreateInterbankDeposit(ctx context.Context, d *InterbankDeposit) (*InterbankDeposit, error) {
	if d.ID == "" {
		d.ID = NewDepositID("ibm")
	}
	if d.Status == "" {
		d.Status = "ACTIVE"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO ibm_deposits (id, tenant_id, deposit_code, product_code, counterparty_code,
			counterparty_name, deposit_date, maturity_date, principal_minor, interest_rate,
			accrued_minor, currency_code, org_code, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,0,$11,$12,$13,$14)
		RETURNING created_at, updated_at`,
		d.ID, d.TenantID, d.DepositCode, nullStringDep(d.ProductCode), d.CounterpartyCode,
		d.CounterpartyName, d.DepositDate, d.MaturityDate, d.PrincipalMinor, d.InterestRate,
		d.CurrencyCode, nullStringDep(d.OrgCode), d.Status, d.CreatedBy)
	if err := row.Scan(&d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return d, nil
}

// SetInterbankDepositCase stamps the workflow case on a staged contract.
func (r *DepositRepository) SetInterbankDepositCase(ctx context.Context, tenantID, id, caseID, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_deposits SET workflow_case_id = NULLIF($3,'')::uuid, updated_by = $4,
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, caseID, actor)
	return err
}

// UpdateInterbankDepositStatus moves the contract between lifecycle states.
func (r *DepositRepository) UpdateInterbankDepositStatus(ctx context.Context, tenantID, id, status, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_deposits SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status, actor)
	return err
}

// SetInterbankDepositJournal stamps the placement posting result.
func (r *DepositRepository) SetInterbankDepositJournal(ctx context.Context, tenantID, id, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_deposits SET journal_entry_id = NULLIF($3,'')::uuid, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, journalEntryID)
	return err
}

// ApplyIBMTopUp increases the deposit principal (TOP_UP).
func (r *DepositRepository) ApplyIBMTopUp(ctx context.Context, tenantID, id string, amountMinor int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_deposits SET principal_minor = principal_minor + $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE'`, tenantID, id, amountMinor)
	return err
}

// ApplyIBMAccrual increases the accrued interest (EXPECTED / thu lãi).
func (r *DepositRepository) ApplyIBMAccrual(ctx context.Context, tenantID, id string, amountMinor int64, interestDate string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_deposits SET accrued_minor = accrued_minor + $3,
			last_interest_date = COALESCE(NULLIF($4,'')::date, last_interest_date),
			updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE'`, tenantID, id, amountMinor, interestDate)
	return err
}

// ApplyIBMWithdraw reduces principal first, then accrued (rút lãi gốc).
func (r *DepositRepository) ApplyIBMWithdraw(ctx context.Context, tenantID, id string, amountMinor int64) error {
	tag, err := r.db.ExecContext(ctx, `
		UPDATE ibm_deposits SET
			principal_minor = GREATEST(principal_minor - $3, 0),
			accrued_minor = GREATEST(accrued_minor - GREATEST($3 - principal_minor, 0), 0),
			updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE' AND principal_minor + accrued_minor >= $3`,
		tenantID, id, amountMinor)
	if err != nil {
		return err
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return fmt.Errorf("insufficient deposit balance or contract not active")
	}
	return nil
}

// ── IBM products ──

// ListIBMProducts returns IBM product rows.
func (r *DepositRepository) ListIBMProducts(ctx context.Context, tenantID string, includeInactive bool) ([]IBMProduct, error) {
	active := " AND is_active"
	if includeInactive {
		active = ""
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, term_months, interest_rate, currency_code, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM ibm_products WHERE tenant_id = $1`+active+` ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IBMProduct{}
	for rows.Next() {
		var p IBMProduct
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.TermMonths, &p.InterestRate,
			&p.CurrencyCode, &p.IsActive, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetIBMProductByCode loads one IBM product by code.
func (r *DepositRepository) GetIBMProductByCode(ctx context.Context, tenantID, code string) (*IBMProduct, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, term_months, interest_rate, currency_code, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM ibm_products WHERE tenant_id = $1 AND code = $2`, tenantID, code)
	var p IBMProduct
	err := row.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.TermMonths, &p.InterestRate,
		&p.CurrencyCode, &p.IsActive, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertIBMProduct creates or updates an IBM product by (tenant, code).
func (r *DepositRepository) UpsertIBMProduct(ctx context.Context, p *IBMProduct) (*IBMProduct, error) {
	if p.ID == "" {
		p.ID = NewDepositID("ibmprd")
	}
	if p.CurrencyCode == "" {
		p.CurrencyCode = "VND"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO ibm_products (id, tenant_id, code, name, term_months, interest_rate,
			currency_code, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name,
			term_months = EXCLUDED.term_months, interest_rate = EXCLUDED.interest_rate,
			currency_code = EXCLUDED.currency_code, is_active = EXCLUDED.is_active,
			updated_at = now(), version = ibm_products.version + 1
		RETURNING created_at, updated_at`,
		p.ID, p.TenantID, p.Code, p.Name, p.TermMonths, p.InterestRate, p.CurrencyCode, p.IsActive, p.CreatedBy)
	if err := row.Scan(&p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ── IBM movements ──

const ibmMovementColumns = `id::text, tenant_id, deposit_id, kind, amount_minor, currency_code,
	movement_date::text, COALESCE(period_from::text,''), COALESCE(period_to::text,''), COALESCE(note,''),
	COALESCE(idempotency_key,''), status, workflow_case_id::text, journal_entry_id::text,
	COALESCE(created_by,''), created_at, updated_at`

func scanIBMMovement(row interface {
	Scan(dest ...any) error
}) (*IBMMovement, error) {
	var m IBMMovement
	var caseID, entryID sql.NullString
	if err := row.Scan(&m.ID, &m.TenantID, &m.DepositID, &m.Kind, &m.AmountMinor, &m.CurrencyCode,
		&m.MovementDate, &m.PeriodFrom, &m.PeriodTo, &m.Note, &m.IdempotencyKey, &m.Status,
		&caseID, &entryID, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	if caseID.Valid {
		m.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		m.JournalEntryID = &entryID.String
	}
	return &m, nil
}

// CreateIBMMovement inserts one staged movement.
func (r *DepositRepository) CreateIBMMovement(ctx context.Context, m *IBMMovement) (*IBMMovement, error) {
	if m.ID == "" {
		m.ID = NewDepositID("ibmmv")
	}
	if m.Status == "" {
		m.Status = "DRAFT"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO ibm_movements (id, tenant_id, deposit_id, kind, amount_minor, currency_code,
			movement_date, period_from, period_to, note, idempotency_key, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,NULLIF($9,'')::date,$10,NULLIF($11,''),$12,$13)
		RETURNING created_at, updated_at`,
		m.ID, m.TenantID, m.DepositID, m.Kind, m.AmountMinor, m.CurrencyCode, m.MovementDate,
		m.PeriodFrom, m.PeriodTo, m.Note, m.IdempotencyKey, m.Status, m.CreatedBy)
	if err := row.Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return m, nil
}

// GetIBMMovementByID loads one movement.
func (r *DepositRepository) GetIBMMovementByID(ctx context.Context, tenantID, id string) (*IBMMovement, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+ibmMovementColumns+` FROM ibm_movements WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	m, err := scanIBMMovement(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListIBMMovementsByDeposit returns movements of one contract, newest first.
func (r *DepositRepository) ListIBMMovementsByDeposit(ctx context.Context, tenantID, depositID string) ([]IBMMovement, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+ibmMovementColumns+`
		FROM ibm_movements WHERE tenant_id = $1 AND deposit_id = $2 ORDER BY created_at DESC`, tenantID, depositID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IBMMovement{}
	for rows.Next() {
		m, err := scanIBMMovement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// SetIBMMovementCase stamps the workflow case + SUBMITTED state.
func (r *DepositRepository) SetIBMMovementCase(ctx context.Context, tenantID, id, caseID, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_movements SET workflow_case_id = NULLIF($3,'')::uuid, status = 'SUBMITTED',
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, caseID)
	return err
}

// SetIBMMovementJournal stamps the posting result.
func (r *DepositRepository) SetIBMMovementJournal(ctx context.Context, tenantID, id, status, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_movements SET status = $3, journal_entry_id = NULLIF($4,'')::uuid,
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, status, journalEntryID)
	return err
}

// SetIBMMovementStatus moves the movement between lifecycle states.
func (r *DepositRepository) SetIBMMovementStatus(ctx context.Context, tenantID, id, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_movements SET status = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id, status)
	return err
}

// ── DPM interest rates / accruals / ops (W3) ──

// InterestRate is one DPM rate tier row.
type InterestRate struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	ProductCode   string    `json:"product_code,omitempty"`
	TermMonths    int       `json:"term_months"`
	Method        string    `json:"method"`
	Denominator   int       `json:"denominator"`
	Rate          float64   `json:"rate"`
	EffectiveFrom string    `json:"effective_from"`
	IsActive      bool      `json:"is_active"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// RateRequest is one staged rate register/adjust request.
type RateRequest struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	RequestType    string          `json:"request_type"`
	Payload        json.RawMessage `json:"payload"`
	Status         string          `json:"status"`
	WorkflowCaseID *string         `json:"workflow_case_id,omitempty"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Accrual is one posted daily-prorated accrual row.
type Accrual struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	SavingsID    string    `json:"savings_id"`
	SavingsCode  string    `json:"savings_code"`
	PeriodFrom   string    `json:"period_from"`
	PeriodTo     string    `json:"period_to"`
	Days         int       `json:"days"`
	BaseMinor    int64     `json:"base_minor"`
	Rate         float64   `json:"rate"`
	AmountMinor  int64     `json:"amount_minor"`
	Status       string    `json:"status"`
	JournalID    *string   `json:"journal_entry_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// InterestOp is one staged interest pay/capitalize operation.
type InterestOp struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	SavingsID      string    `json:"savings_id"`
	SavingsCode    string    `json:"savings_code"`
	OpType         string    `json:"op_type"`
	AmountMinor    int64     `json:"amount_minor"`
	Days           int       `json:"days"`
	Rate           float64   `json:"rate"`
	PeriodFrom     string    `json:"period_from,omitempty"`
	PeriodTo       string    `json:"period_to,omitempty"`
	BatchID        *string   `json:"batch_id,omitempty"`
	Status         string    `json:"status"`
	WorkflowCaseID *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID *string   `json:"journal_entry_id,omitempty"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ListInterestRates returns active rate tiers (optionally for one product).
func (r *DepositRepository) ListInterestRates(ctx context.Context, tenantID, productCode string) ([]InterestRate, error) {
	where := []string{"tenant_id = $1", "is_active"}
	args := []any{tenantID}
	if productCode != "" {
		args = append(args, productCode)
		where = append(where, fmt.Sprintf("(product_code = $%d OR product_code IS NULL)", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, COALESCE(product_code,''), term_months, method, denominator, rate,
		       effective_from::text, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM dpm_interest_rates WHERE `+strings.Join(where, " AND ")+`
		ORDER BY effective_from DESC, term_months ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterestRate{}
	for rows.Next() {
		var x InterestRate
		if err := rows.Scan(&x.ID, &x.TenantID, &x.ProductCode, &x.TermMonths, &x.Method, &x.Denominator,
			&x.Rate, &x.EffectiveFrom, &x.IsActive, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// FindEffectiveRate returns the most specific active tier for the product/term
// on or before onDate (falls back product → default bucket).
func (r *DepositRepository) FindEffectiveRate(ctx context.Context, tenantID, productCode string, termMonths int, onDate string) (*InterestRate, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, COALESCE(product_code,''), term_months, method, denominator, rate,
		       effective_from::text, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM dpm_interest_rates
		WHERE tenant_id = $1 AND is_active AND effective_from <= $2::date
		  AND (product_code = $3 OR product_code IS NULL)
		  AND (term_months = $4 OR term_months = 0)
		ORDER BY (product_code = $3) DESC, (term_months = $4) DESC, effective_from DESC
		LIMIT 1`, tenantID, onDate, nullStringDep(productCode), termMonths)
	var x InterestRate
	err := row.Scan(&x.ID, &x.TenantID, &x.ProductCode, &x.TermMonths, &x.Method, &x.Denominator,
		&x.Rate, &x.EffectiveFrom, &x.IsActive, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// UpsertInterestRate inserts one rate tier (bucket + effective date unique).
func (r *DepositRepository) UpsertInterestRate(ctx context.Context, in *InterestRate) (*InterestRate, error) {
	if in.ID == "" {
		in.ID = NewDepositID("dpmrate")
	}
	if in.Method == "" {
		in.Method = "SIMPLE"
	}
	if in.Denominator == 0 {
		in.Denominator = 365
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dpm_interest_rates (id, tenant_id, product_code, term_months, method, denominator,
			rate, effective_from, is_active, created_by)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,true,$9)
		ON CONFLICT (tenant_id, COALESCE(product_code, ''), term_months, effective_from)
		DO UPDATE SET rate = EXCLUDED.rate, method = EXCLUDED.method,
			denominator = EXCLUDED.denominator, is_active = true,
			updated_at = now(), version = dpm_interest_rates.version + 1
		RETURNING created_at, updated_at`,
		in.ID, in.TenantID, in.ProductCode, in.TermMonths, in.Method, in.Denominator,
		in.Rate, in.EffectiveFrom, in.CreatedBy)
	if err := row.Scan(&in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

// CreateRateRequest stages one rate request row.
func (r *DepositRepository) CreateRateRequest(ctx context.Context, in *RateRequest) (*RateRequest, error) {
	if len(in.Payload) == 0 {
		in.Payload = json.RawMessage(`{}`)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dpm_rate_requests (tenant_id, request_type, payload, status, created_by)
		VALUES ($1,$2,$3,$4,$5) RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.RequestType, in.Payload, in.Status, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

// GetRateRequestByID loads one rate request.
func (r *DepositRepository) GetRateRequestByID(ctx context.Context, tenantID, id string) (*RateRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, tenant_id, request_type, payload, status, workflow_case_id::text,
		       COALESCE(created_by,''), created_at, updated_at
		FROM dpm_rate_requests WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	var x RateRequest
	var caseID sql.NullString
	err := row.Scan(&x.ID, &x.TenantID, &x.RequestType, &x.Payload, &x.Status, &caseID,
		&x.CreatedBy, &x.CreatedAt, &x.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if caseID.Valid {
		x.WorkflowCaseID = &caseID.String
	}
	return &x, nil
}

// SetRateRequestCase stamps the workflow case + SUBMITTED state.
func (r *DepositRepository) SetRateRequestCase(ctx context.Context, tenantID, id, caseID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_rate_requests SET workflow_case_id = NULLIF($3,'')::uuid, status = 'SUBMITTED',
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, caseID)
	return err
}

// SetRateRequestStatus moves the request between lifecycle states.
func (r *DepositRepository) SetRateRequestStatus(ctx context.Context, tenantID, id, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_rate_requests SET status = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id, status)
	return err
}

// CreateAccrual inserts one accrual row; returns false when the period was
// already accrued (idempotent per savings+period_to).
func (r *DepositRepository) CreateAccrual(ctx context.Context, a *Accrual) (bool, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dpm_accruals (tenant_id, savings_id, savings_code, period_from, period_to, days,
			base_minor, rate, amount_minor, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'POSTED')
		ON CONFLICT (tenant_id, savings_id, period_to) DO NOTHING
		RETURNING id::text`,
		a.TenantID, a.SavingsID, a.SavingsCode, a.PeriodFrom, a.PeriodTo, a.Days,
		a.BaseMinor, a.Rate, a.AmountMinor)
	var id string
	err := row.Scan(&id)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	a.ID = id
	return true, nil
}

// SetAccrualJournal stamps the accrual posting result.
func (r *DepositRepository) SetAccrualJournal(ctx context.Context, tenantID, id, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_accruals SET journal_entry_id = NULLIF($3,'')::uuid WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, journalEntryID)
	return err
}

// LastAccrualPeriod returns the last accrued-to date for one savings.
func (r *DepositRepository) LastAccrualPeriod(ctx context.Context, tenantID, savingsID string) (string, error) {
	var last sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT MAX(period_to)::text FROM dpm_accruals WHERE tenant_id = $1 AND savings_id = $2`,
		tenantID, savingsID).Scan(&last)
	if err != nil {
		return "", err
	}
	return last.String, nil
}

// ListAccrualsBySavings returns accrual rows for one savings account.
func (r *DepositRepository) ListAccrualsBySavings(ctx context.Context, tenantID, savingsID string) ([]Accrual, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, savings_id, savings_code, period_from::text, period_to::text, days,
		       base_minor, rate, amount_minor, status, journal_entry_id::text, created_at
		FROM dpm_accruals WHERE tenant_id = $1 AND savings_id = $2 ORDER BY period_to DESC LIMIT 100`,
		tenantID, savingsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Accrual{}
	for rows.Next() {
		var a Accrual
		var journal sql.NullString
		if err := rows.Scan(&a.ID, &a.TenantID, &a.SavingsID, &a.SavingsCode, &a.PeriodFrom, &a.PeriodTo,
			&a.Days, &a.BaseMinor, &a.Rate, &a.AmountMinor, &a.Status, &journal, &a.CreatedAt); err != nil {
			return nil, err
		}
		if journal.Valid {
			a.JournalID = &journal.String
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListActiveSavingsForAccrual returns ACTIVE savings ordered for COB accrual.
func (r *DepositRepository) ListActiveSavingsForAccrual(ctx context.Context, tenantID string) ([]Savings, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, savings_code, customer_code, product_code, open_date::text, maturity_date::text,
		       principal_minor, accrued_minor, currency_code, COALESCE(org_code,''), status,
		       workflow_case_id::text, journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at
		FROM dpm_savings WHERE tenant_id = $1 AND status = 'ACTIVE' ORDER BY savings_code`, tenantID)
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

// ApplyAccrualToSavings adds accrued interest to the savings row.
func (r *DepositRepository) ApplyAccrualToSavings(ctx context.Context, tenantID, savingsID string, amountMinor int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_savings SET accrued_minor = accrued_minor + $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE'`, tenantID, savingsID, amountMinor)
	return err
}

// ApplyInterestOp updates the savings row for PAY (accrued down) or
// CAPITALIZE (accrued down + principal up).
func (r *DepositRepository) ApplyInterestOp(ctx context.Context, tenantID, savingsID string, amountMinor int64, capitalize bool) error {
	capitalizeSQL := ""
	if capitalize {
		capitalizeSQL = ", principal_minor = principal_minor + $3"
	}
	tag, err := r.db.ExecContext(ctx, `
		UPDATE dpm_savings SET accrued_minor = GREATEST(accrued_minor - $3, 0)`+capitalizeSQL+`,
			updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2 AND status = 'ACTIVE'`, tenantID, savingsID, amountMinor)
	if err != nil {
		return err
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return fmt.Errorf("savings account not active")
	}
	return nil
}

// CreateInterestOp inserts one staged interest op.
func (r *DepositRepository) CreateInterestOp(ctx context.Context, in *InterestOp) (*InterestOp, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO dpm_interest_ops (tenant_id, savings_id, savings_code, op_type, amount_minor, days,
			rate, period_from, period_to, batch_id, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::date,NULLIF($9,'')::date,$10::uuid,$11,$12)
		RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.SavingsID, in.SavingsCode, in.OpType, in.AmountMinor, in.Days, in.Rate,
		in.PeriodFrom, in.PeriodTo, nullStringDep(derefString(in.BatchID)), in.Status, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

// GetInterestOpByID loads one interest op.
func (r *DepositRepository) GetInterestOpByID(ctx context.Context, tenantID, id string) (*InterestOp, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, tenant_id, savings_id, savings_code, op_type, amount_minor, days, rate,
		       COALESCE(period_from::text,''), COALESCE(period_to::text,''), batch_id::text,
		       status, workflow_case_id::text, journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at
		FROM dpm_interest_ops WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	var x InterestOp
	var batchID, caseID, entryID sql.NullString
	err := row.Scan(&x.ID, &x.TenantID, &x.SavingsID, &x.SavingsCode, &x.OpType, &x.AmountMinor, &x.Days,
		&x.Rate, &x.PeriodFrom, &x.PeriodTo, &batchID, &x.Status, &caseID, &entryID, &x.CreatedBy, &x.CreatedAt, &x.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if batchID.Valid {
		x.BatchID = &batchID.String
	}
	if caseID.Valid {
		x.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		x.JournalEntryID = &entryID.String
	}
	return &x, nil
}

// ListInterestOpsBySavings returns ops for one savings account.
func (r *DepositRepository) ListInterestOpsBySavings(ctx context.Context, tenantID, savingsID string) ([]InterestOp, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, savings_id, savings_code, op_type, amount_minor, days, rate,
		       COALESCE(period_from::text,''), COALESCE(period_to::text,''), batch_id::text,
		       status, workflow_case_id::text, journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at
		FROM dpm_interest_ops WHERE tenant_id = $1 AND savings_id = $2 ORDER BY created_at DESC LIMIT 100`,
		tenantID, savingsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterestOp{}
	for rows.Next() {
		var x InterestOp
		var batchID, caseID, entryID sql.NullString
		if err := rows.Scan(&x.ID, &x.TenantID, &x.SavingsID, &x.SavingsCode, &x.OpType, &x.AmountMinor,
			&x.Days, &x.Rate, &x.PeriodFrom, &x.PeriodTo, &batchID, &x.Status, &caseID, &entryID,
			&x.CreatedBy, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		if batchID.Valid {
			x.BatchID = &batchID.String
		}
		if caseID.Valid {
			x.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			x.JournalEntryID = &entryID.String
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ListInterestOpsByBatch returns ops of one batch.
func (r *DepositRepository) ListInterestOpsByBatch(ctx context.Context, tenantID, batchID string) ([]InterestOp, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, savings_id, savings_code, op_type, amount_minor, days, rate,
		       COALESCE(period_from::text,''), COALESCE(period_to::text,''), batch_id::text,
		       status, workflow_case_id::text, journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at
		FROM dpm_interest_ops WHERE tenant_id = $1 AND batch_id = $2::uuid ORDER BY savings_code`,
		tenantID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterestOp{}
	for rows.Next() {
		var x InterestOp
		var batch, caseID, entryID sql.NullString
		if err := rows.Scan(&x.ID, &x.TenantID, &x.SavingsID, &x.SavingsCode, &x.OpType, &x.AmountMinor,
			&x.Days, &x.Rate, &x.PeriodFrom, &x.PeriodTo, &batch, &x.Status, &caseID, &entryID,
			&x.CreatedBy, &x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		if batch.Valid {
			x.BatchID = &batch.String
		}
		if caseID.Valid {
			x.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			x.JournalEntryID = &entryID.String
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SetInterestOpCase stamps the workflow case + SUBMITTED state.
func (r *DepositRepository) SetInterestOpCase(ctx context.Context, tenantID, id, caseID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_interest_ops SET workflow_case_id = NULLIF($3,'')::uuid, status = 'SUBMITTED',
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, caseID)
	return err
}

// SetInterestOpJournal stamps the posting result.
func (r *DepositRepository) SetInterestOpJournal(ctx context.Context, tenantID, id, status, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_interest_ops SET status = $3, journal_entry_id = NULLIF($4,'')::uuid,
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, status, journalEntryID)
	return err
}

// SetInterestOpStatus moves the op between lifecycle states.
func (r *DepositRepository) SetInterestOpStatus(ctx context.Context, tenantID, id, status string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE dpm_interest_ops SET status = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id, status)
	return err
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ── Reports (W4c) ──

// SavingsStatementRow is one sổ tiền gửi report row.
type SavingsStatementRow struct {
	SavingsCode    string `json:"savings_code"`
	CustomerCode   string `json:"customer_code"`
	ProductCode    string `json:"product_code"`
	OpenDate       string `json:"open_date"`
	MaturityDate   string `json:"maturity_date"`
	PrincipalMinor int64  `json:"principal_minor"`
	AccruedMinor   int64  `json:"accrued_minor"`
	CurrencyCode   string `json:"currency_code"`
	Status         string `json:"status"`
}

// SavingsTxnRow is one giao dịch tiền gửi report row.
type SavingsTxnRow struct {
	TxnDate        string `json:"txn_date"`
	SavingsCode    string `json:"savings_code"`
	TxnType        string `json:"txn_type"`
	AmountMinor    int64  `json:"amount_minor"`
	CurrencyCode   string `json:"currency_code"`
	Status         string `json:"status"`
	JournalEntryID string `json:"journal_entry_id,omitempty"`
}

// InterbankStatementRow is one sổ tiền gửi liên ngân hàng report row.
type InterbankStatementRow struct {
	DepositCode      string `json:"deposit_code"`
	CounterpartyCode string `json:"counterparty_code"`
	CounterpartyName string `json:"counterparty_name,omitempty"`
	ProductCode      string `json:"product_code,omitempty"`
	DepositDate      string `json:"deposit_date"`
	MaturityDate     string `json:"maturity_date"`
	PrincipalMinor   int64  `json:"principal_minor"`
	AccruedMinor     int64  `json:"accrued_minor"`
	CurrencyCode     string `json:"currency_code"`
	Status           string `json:"status"`
}

// InterbankTxnRow is one giao dịch liên ngân hàng report row.
type InterbankTxnRow struct {
	MovementDate string `json:"movement_date"`
	DepositCode  string `json:"deposit_code"`
	Kind         string `json:"kind"`
	AmountMinor  int64  `json:"amount_minor"`
	CurrencyCode string `json:"currency_code"`
	Note         string `json:"note,omitempty"`
	Status       string `json:"status"`
}

// SavingsStatement lists savings accounts opened on/before toDate.
func (r *DepositRepository) SavingsStatement(ctx context.Context, tenantID, fromDate, toDate, status string) ([]SavingsStatementRow, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if toDate != "" {
		args = append(args, toDate)
		where = append(where, fmt.Sprintf("open_date <= $%d::date", len(args)))
	}
	if fromDate != "" {
		args = append(args, fromDate)
		where = append(where, fmt.Sprintf("open_date >= $%d::date", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT savings_code, customer_code, product_code, open_date::text, maturity_date::text,
		       principal_minor, accrued_minor, currency_code, status
		FROM dpm_savings WHERE `+strings.Join(where, " AND ")+`
		ORDER BY open_date, savings_code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavingsStatementRow{}
	for rows.Next() {
		var x SavingsStatementRow
		if err := rows.Scan(&x.SavingsCode, &x.CustomerCode, &x.ProductCode, &x.OpenDate, &x.MaturityDate,
			&x.PrincipalMinor, &x.AccruedMinor, &x.CurrencyCode, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SavingsTransactions lists deposit transactions in [from, to].
func (r *DepositRepository) SavingsTransactions(ctx context.Context, tenantID, fromDate, toDate, txnType string) ([]SavingsTxnRow, error) {
	where := []string{"t.tenant_id = $1"}
	args := []any{tenantID}
	if fromDate != "" {
		args = append(args, fromDate)
		where = append(where, fmt.Sprintf("t.txn_date >= $%d::date", len(args)))
	}
	if toDate != "" {
		args = append(args, toDate)
		where = append(where, fmt.Sprintf("t.txn_date <= $%d::date", len(args)))
	}
	if txnType != "" {
		args = append(args, txnType)
		where = append(where, fmt.Sprintf("t.txn_type = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.txn_date::text, s.savings_code, t.txn_type, t.amount_minor, t.currency_code, t.status,
		       COALESCE(t.journal_entry_id::text,'')
		FROM dpm_transactions t JOIN dpm_savings s ON s.id = t.savings_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY t.txn_date DESC, s.savings_code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavingsTxnRow{}
	for rows.Next() {
		var x SavingsTxnRow
		if err := rows.Scan(&x.TxnDate, &x.SavingsCode, &x.TxnType, &x.AmountMinor, &x.CurrencyCode,
			&x.Status, &x.JournalEntryID); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// InterbankStatement lists interbank contracts opened on/before toDate.
func (r *DepositRepository) InterbankStatement(ctx context.Context, tenantID, fromDate, toDate, status string) ([]InterbankStatementRow, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if fromDate != "" {
		args = append(args, fromDate)
		where = append(where, fmt.Sprintf("deposit_date >= $%d::date", len(args)))
	}
	if toDate != "" {
		args = append(args, toDate)
		where = append(where, fmt.Sprintf("deposit_date <= $%d::date", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT deposit_code, counterparty_code, COALESCE(counterparty_name,''), COALESCE(product_code,''),
		       deposit_date::text, maturity_date::text, principal_minor, accrued_minor, currency_code, status
		FROM ibm_deposits WHERE `+strings.Join(where, " AND ")+`
		ORDER BY deposit_date DESC LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterbankStatementRow{}
	for rows.Next() {
		var x InterbankStatementRow
		if err := rows.Scan(&x.DepositCode, &x.CounterpartyCode, &x.CounterpartyName, &x.ProductCode,
			&x.DepositDate, &x.MaturityDate, &x.PrincipalMinor, &x.AccruedMinor, &x.CurrencyCode, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// InterbankTransactions lists interbank movements in [from, to].
func (r *DepositRepository) InterbankTransactions(ctx context.Context, tenantID, fromDate, toDate string) ([]InterbankTxnRow, error) {
	where := []string{"m.tenant_id = $1"}
	args := []any{tenantID}
	if fromDate != "" {
		args = append(args, fromDate)
		where = append(where, fmt.Sprintf("m.movement_date >= $%d::date", len(args)))
	}
	if toDate != "" {
		args = append(args, toDate)
		where = append(where, fmt.Sprintf("m.movement_date <= $%d::date", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.movement_date::text, d.deposit_code, m.kind, m.amount_minor, m.currency_code,
		       COALESCE(m.note,''), m.status
		FROM ibm_movements m JOIN ibm_deposits d ON d.id = m.deposit_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY m.movement_date DESC, d.deposit_code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterbankTxnRow{}
	for rows.Next() {
		var x InterbankTxnRow
		if err := rows.Scan(&x.MovementDate, &x.DepositCode, &x.Kind, &x.AmountMinor, &x.CurrencyCode,
			&x.Note, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
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
