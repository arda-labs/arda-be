package repository

import (
	"context"
	cryptoRandCap "crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// FundType is one capital fund classification.
type FundType struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CapitalContract is one fund contract (EPAS CFM contract-formation).
type CapitalContract struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	ContractCode     string    `json:"contract_code"`
	FundTypeCode     string    `json:"fund_type_code"`
	CounterpartyCode string    `json:"counterparty_code"`
	ContractDate     string    `json:"contract_date"`
	AmountMinor      int64     `json:"amount_minor"`
	InterestRate     float64   `json:"interest_rate"`
	CurrencyCode     string    `json:"currency_code"`
	Status           string    `json:"status"`
	OrgCode          string    `json:"org_code,omitempty"`
	WorkflowCaseID   *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID   *string   `json:"journal_entry_id,omitempty"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// CapitalMovement is one fund movement (receipt/disbursement/payment).
type CapitalMovement struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ContractID     string    `json:"contract_id"`
	MovementType   string    `json:"movement_type"`
	AmountMinor    int64     `json:"amount_minor"`
	CurrencyCode   string    `json:"currency_code"`
	MovementDate   string    `json:"movement_date"`
	Status         string    `json:"status"`
	WorkflowCaseID *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID *string   `json:"journal_entry_id,omitempty"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
}

// CapitalRepository persists capital domain data.
type CapitalRepository struct {
	db *sql.DB
}

func NewCapitalRepository(db *sql.DB) *CapitalRepository {
	return &CapitalRepository{db: db}
}

// ListFundTypes returns active fund types.
func (r *CapitalRepository) ListFundTypes(ctx context.Context, tenantID string) ([]FundType, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM cfc_fund_types WHERE tenant_id IN ('', $1) AND is_active ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FundType{}
	for rows.Next() {
		var t FundType
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Code, &t.Name, &t.IsActive, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListContracts returns fund contracts.
func (r *CapitalRepository) ListContracts(ctx context.Context, tenantID string, orgCodes []string, status string) ([]CapitalContract, error) {
	orgAny := any(nil)
	if len(orgCodes) > 0 {
		orgAny = orgCodes
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, contract_code, fund_type_code, counterparty_code, contract_date::text,
		       amount_minor, interest_rate, currency_code, status, COALESCE(org_code,''),
		       workflow_case_id::text, journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at
		FROM cfc_contracts
		WHERE tenant_id = $1
		  AND ($4::text = '' OR status = $4::text)
		  AND ($5::text[] IS NULL OR org_code = ANY($5::text[]))
		ORDER BY contract_date DESC LIMIT 200`, tenantID, orgAny, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CapitalContract{}
	for rows.Next() {
		var c CapitalContract
		var caseID, entryID sql.NullString
		if err := rows.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.FundTypeCode, &c.CounterpartyCode,
			&c.ContractDate, &c.AmountMinor, &c.InterestRate, &c.CurrencyCode, &c.Status, &c.OrgCode,
			&caseID, &entryID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
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

// CreateContract inserts a fund contract.
func (r *CapitalRepository) CreateContract(ctx context.Context, c *CapitalContract) (*CapitalContract, error) {
	if c.ID == "" {
		c.ID = NewCapitalID("cfc")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO cfc_contracts (id, tenant_id, contract_code, fund_type_code, counterparty_code,
			contract_date, amount_minor, interest_rate, currency_code, status, org_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'ACTIVE',$10,$11)
		RETURNING created_at, updated_at`,
		c.ID, c.TenantID, c.ContractCode, c.FundTypeCode, c.CounterpartyCode, c.ContractDate,
		c.AmountMinor, c.InterestRate, c.CurrencyCode, nullStringCap(c.OrgCode), c.CreatedBy)
	if err := row.Scan(&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return c, nil
}

// RecordMovement inserts one movement row.
func (r *CapitalRepository) RecordMovement(ctx context.Context, m *CapitalMovement) (*CapitalMovement, error) {
	if m.ID == "" {
		m.ID = NewCapitalID("cfcmv")
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO cfc_movements (id, tenant_id, contract_id, movement_type, amount_minor,
			currency_code, movement_date, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'DRAFT',$8)
		RETURNING created_at`,
		m.ID, m.TenantID, m.ContractID, m.MovementType, m.AmountMinor, m.CurrencyCode,
		m.MovementDate, m.CreatedBy)
	if err := row.Scan(&m.CreatedAt); err != nil {
		return nil, err
	}
	return m, nil
}

// SetMovementJournal stamps the posting result.
func (r *CapitalRepository) SetMovementJournal(ctx context.Context, tenantID, id, status, journalEntryID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_movements SET status = $3, journal_entry_id = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status, nullStringCap(journalEntryID))
	return err
}

// GetContractByID loads one contract.
func (r *CapitalRepository) GetContractByID(ctx context.Context, tenantID, id string) (*CapitalContract, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, contract_code, fund_type_code, counterparty_code, contract_date::text,
		       amount_minor, interest_rate, currency_code, status, COALESCE(org_code,''),
		       workflow_case_id::text, journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at
		FROM cfc_contracts WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	var c CapitalContract
	var caseID, entryID sql.NullString
	err := row.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.FundTypeCode, &c.CounterpartyCode,
		&c.ContractDate, &c.AmountMinor, &c.InterestRate, &c.CurrencyCode, &c.Status, &c.OrgCode,
		&caseID, &entryID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("contract not found")
	}
	if err != nil {
		return nil, err
	}
	if caseID.Valid {
		c.WorkflowCaseID = &caseID.String
	}
	if entryID.Valid {
		c.JournalEntryID = &entryID.String
	}
	return &c, nil
}

// NewCapitalID generates a prefixed random id.
func NewCapitalID(prefix string) string {
	var b [16]byte
	if _, err := cryptoRandCap.Read(b[:]); err != nil {
		panic("capital id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func nullStringCap(s string) any {
	if s == "" {
		return nil
	}
	return s
}
