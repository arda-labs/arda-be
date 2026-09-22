package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// InterbankBorrow is one interbank BORROWING contract (tiền vay TCTD khác) —
// the mirror of InterbankDeposit. The QTDND is the borrower here.
type InterbankBorrow struct {
	ID               string `json:"id"`
	TenantID         string `json:"tenant_id"`
	BorrowCode       string `json:"borrow_code"`
	CounterpartyCode string `json:"counterparty_code"`
	CounterpartyName string `json:"counterparty_name,omitempty"`
	ProductCode      string `json:"product_code,omitempty"`
	// NHHTX | NHNN | OTHER_TCTD | SAFETY_FUND
	LenderType string `json:"lender_type"`
	// CREDIT_EXPANSION | DEPOSIT_PAYMENT | DIFFICULTY | SPECIAL | OTHER
	FundingPurpose   string    `json:"funding_purpose"`
	TermMonths       int       `json:"term_months"`
	DrawdownDate     string    `json:"drawdown_date"`
	MaturityDate     string    `json:"maturity_date"`
	PrincipalMinor   int64     `json:"principal_minor"`
	OutstandingMinor int64     `json:"outstanding_minor"`
	AccruedMinor     int64     `json:"accrued_minor"`
	InterestRate     float64   `json:"interest_rate"`
	CurrencyCode     string    `json:"currency_code"`
	OrgCode          string    `json:"org_code,omitempty"`
	Status           string    `json:"status"`
	LastInterestDate string    `json:"last_interest_date,omitempty"`
	WorkflowCaseID   *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID   *string   `json:"journal_entry_id,omitempty"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	DataVersion      int64     `json:"data_version"`
}

// IBMBorrowMovement is one staged/stamped movement against an interbank borrow.
type IBMBorrowMovement struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	BorrowID       string    `json:"borrow_id"`
	Kind           string    `json:"kind"` // DRAWDOWN|REPAYMENT|INTEREST|EARLY_REPAY
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
	DataVersion    int64     `json:"data_version"`
}

const borrowColumns = `id, tenant_id, borrow_code, counterparty_code, COALESCE(counterparty_name,''),
	COALESCE(product_code,''), lender_type, funding_purpose, term_months,
	drawdown_date::text, maturity_date::text, principal_minor, outstanding_minor, accrued_minor,
	interest_rate, currency_code, COALESCE(org_code,''), status,
	COALESCE(last_interest_date::text,''), workflow_case_id::text, journal_entry_id::text,
	COALESCE(created_by,''), created_at, updated_at, version`

func scanBorrow(row interface{ Scan(...any) error }) (InterbankBorrow, error) {
	var b InterbankBorrow
	var caseID, entryID sql.NullString
	err := row.Scan(&b.ID, &b.TenantID, &b.BorrowCode, &b.CounterpartyCode, &b.CounterpartyName,
		&b.ProductCode, &b.LenderType, &b.FundingPurpose, &b.TermMonths,
		&b.DrawdownDate, &b.MaturityDate, &b.PrincipalMinor, &b.OutstandingMinor, &b.AccruedMinor,
		&b.InterestRate, &b.CurrencyCode, &b.OrgCode, &b.Status,
		&b.LastInterestDate, &caseID, &entryID, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.DataVersion)
	if err != nil {
		return b, err
	}
	if caseID.Valid {
		b.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		b.JournalEntryID = &entryID.String
	}
	return b, nil
}

// CreateIBMBorrow inserts a borrow contract with the supplied status.
func (r *DepositRepository) CreateIBMBorrow(ctx context.Context, b *InterbankBorrow) (*InterbankBorrow, error) {
	if b.ID == "" {
		b.ID = NewDepositID("ibmb")
	}
	if b.Status == "" {
		b.Status = "PENDING_APPROVAL"
	}
	if b.CurrencyCode == "" {
		b.CurrencyCode = "VND"
	}
	if b.LenderType == "" {
		b.LenderType = "OTHER_TCTD"
	}
	if b.FundingPurpose == "" {
		b.FundingPurpose = "OTHER"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO ibm_borrows (id, tenant_id, borrow_code, counterparty_code, counterparty_name,
			product_code, lender_type, funding_purpose, term_months, drawdown_date, maturity_date,
			principal_minor, outstanding_minor, accrued_minor, interest_rate, currency_code,
			org_code, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12,0,$13,$14,$15,$16,$17)
		RETURNING created_at, updated_at`,
		b.ID, b.TenantID, b.BorrowCode, b.CounterpartyCode, b.CounterpartyName,
		b.ProductCode, b.LenderType, b.FundingPurpose, b.TermMonths, b.DrawdownDate, b.MaturityDate,
		b.PrincipalMinor, b.InterestRate, b.CurrencyCode,
		nullStringDep(b.OrgCode), b.Status, b.CreatedBy)
	if err := row.Scan(&b.CreatedAt, &b.UpdatedAt); err != nil {
		return nil, err
	}
	b.OutstandingMinor = b.PrincipalMinor
	return b, nil
}

// GetIBMBorrowByID loads one borrow contract.
func (r *DepositRepository) GetIBMBorrowByID(ctx context.Context, tenantID, id string) (*InterbankBorrow, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+borrowColumns+` FROM ibm_borrows WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	b, err := scanBorrow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ListIBMBorrows returns the tenant's borrow contracts, optionally one org or
// status. Bounded like the deposit list.
func (r *DepositRepository) ListIBMBorrows(ctx context.Context, tenantID string, orgCodes []string, status string) ([]InterbankBorrow, error) {
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
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM ibm_borrows WHERE %s ORDER BY drawdown_date DESC LIMIT 200`,
		borrowColumns, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterbankBorrow{}
	for rows.Next() {
		b, err := scanBorrow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpdateIBMBorrowStatus moves a borrow between lifecycle states. dataVersion
// (when set) is the row version the checker approved: the update is guarded so a
// stale approval cannot move a contract that changed.
func (r *DepositRepository) UpdateIBMBorrowStatus(ctx context.Context, tenantID, id, status, actor, dataVersion string) error {
	tag, err := r.db.ExecContext(ctx, `
		UPDATE ibm_borrows SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2
		  AND ($5 = '' OR version::text = $5)`, tenantID, id, status, actor, dataVersion)
	if err != nil {
		return err
	}
	if n, _ := tag.RowsAffected(); n == 0 {
		return ErrIBMNotActionable
	}
	return nil
}

// SetIBMBorrowCase stamps the workflow case on a staged borrow.
func (r *DepositRepository) SetIBMBorrowCase(ctx context.Context, tenantID, id, caseID, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE ibm_borrows SET workflow_case_id = NULLIF($3,'')::uuid, updated_by = $4,
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, caseID, actor)
	return err
}

// CreateIBMBorrowMovement stages one movement against a borrow.
func (r *DepositRepository) CreateIBMBorrowMovement(ctx context.Context, m *IBMBorrowMovement) (*IBMBorrowMovement, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO ibm_borrow_movements (tenant_id, borrow_id, kind, amount_minor, currency_code,
			movement_date, period_from, period_to, note, idempotency_key, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id, created_at, updated_at`,
		m.TenantID, m.BorrowID, m.Kind, m.AmountMinor, m.CurrencyCode, m.MovementDate,
		nullStringDep(m.PeriodFrom), nullStringDep(m.PeriodTo), m.Note, nullStringDep(m.IdempotencyKey),
		m.Status, m.CreatedBy)
	if err := row.Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return m, nil
}

// ListIBMBorrowMovementsByBorrow returns the movements of one borrow.
func (r *DepositRepository) ListIBMBorrowMovementsByBorrow(ctx context.Context, tenantID, borrowID string) ([]IBMBorrowMovement, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, borrow_id, kind, amount_minor, currency_code,
		       movement_date::text, COALESCE(period_from::text,''), COALESCE(period_to::text,''),
		       note, status, workflow_case_id::text, journal_entry_id::text,
		       created_by, created_at, updated_at, version
		FROM ibm_borrow_movements
		WHERE tenant_id = $1 AND borrow_id = $2
		ORDER BY movement_date, created_at`, tenantID, borrowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IBMBorrowMovement{}
	for rows.Next() {
		var m IBMBorrowMovement
		var caseID, entryID sql.NullString
		if err := rows.Scan(&m.ID, &m.TenantID, &m.BorrowID, &m.Kind, &m.AmountMinor, &m.CurrencyCode,
			&m.MovementDate, &m.PeriodFrom, &m.PeriodTo, &m.Note, &m.Status,
			&caseID, &entryID, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.DataVersion); err != nil {
			return nil, err
		}
		if caseID.Valid {
			m.WorkflowCaseID = &caseID.String
		}
		if entryID.Valid {
			m.JournalEntryID = &entryID.String
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IBMBorrowReportingRow is one borrow in the reporting projection: the business
// fields the PCF "Tiền vay TCTD" indicators read.
type IBMBorrowReportingRow struct {
	BorrowCode       string  `json:"borrow_code"`
	CounterpartyCode string  `json:"counterparty_code"`
	LenderType       string  `json:"lender_type"`
	FundingPurpose   string  `json:"funding_purpose"`
	TermMonths       int     `json:"term_months"`
	DrawdownDate     string  `json:"drawdown_date"`
	MaturityDate     string  `json:"maturity_date"`
	PrincipalMinor   int64   `json:"principal_minor"`
	OutstandingMinor int64   `json:"outstanding_minor"`
	AccruedMinor     int64   `json:"accrued_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	OrgCode          string  `json:"org_code"`
	Status           string  `json:"status"`
}

// ListIBMBorrowsForReporting returns every borrow of the tenant for the ETL.
func (r *DepositRepository) ListIBMBorrowsForReporting(ctx context.Context, tenantID, orgCode string) ([]IBMBorrowReportingRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT borrow_code, counterparty_code, lender_type, funding_purpose, term_months,
		       drawdown_date::text, maturity_date::text, principal_minor, outstanding_minor,
		       accrued_minor, interest_rate, currency_code, COALESCE(org_code,''), status
		FROM ibm_borrows
		WHERE tenant_id = $1 AND ($2 = '' OR org_code = $2)
		ORDER BY borrow_code`, tenantID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IBMBorrowReportingRow{}
	for rows.Next() {
		var x IBMBorrowReportingRow
		if err := rows.Scan(&x.BorrowCode, &x.CounterpartyCode, &x.LenderType, &x.FundingPurpose,
			&x.TermMonths, &x.DrawdownDate, &x.MaturityDate, &x.PrincipalMinor, &x.OutstandingMinor,
			&x.AccruedMinor, &x.InterestRate, &x.CurrencyCode, &x.OrgCode, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
