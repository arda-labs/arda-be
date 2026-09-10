package repository

import (
	"context"
	cryptoRand "crypto/rand"
	"database/sql"
	"encoding/hex"
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
