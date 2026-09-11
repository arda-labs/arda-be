package repository

import (
	"context"
	cryptoRandCap "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
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

// CapitalProduct is one fund product catalog row (EPAS CFM "Sản phẩm (vốn)").
type CapitalProduct struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	FundTypeCode string    `json:"fund_type_code"`
	TermMonths   int       `json:"term_months"`
	InterestRate float64   `json:"interest_rate"`
	CurrencyCode string    `json:"currency_code"`
	IsActive     bool      `json:"is_active"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CapitalContract is one fund contract (EPAS CFM contract-formation).
type CapitalContract struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	ContractCode     string    `json:"contract_code"`
	FundTypeCode     string    `json:"fund_type_code"`
	ProductCode      string    `json:"product_code,omitempty"`
	CounterpartyCode string    `json:"counterparty_code"`
	ContractDate     string    `json:"contract_date"`
	MaturityDate     string    `json:"maturity_date,omitempty"`
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

// ContractAmendment is one staged amendment request for a fund contract.
type ContractAmendment struct {
	ID             string          `json:"id"`
	TenantID       string          `json:"tenant_id"`
	ContractID     string          `json:"contract_id"`
	Status         string          `json:"status"`
	Payload        json.RawMessage `json:"payload"`
	Reason         string          `json:"reason,omitempty"`
	WorkflowCaseID *string         `json:"workflow_case_id,omitempty"`
	SubmittedBy    string          `json:"submitted_by,omitempty"`
	SubmittedAt    *time.Time      `json:"submitted_at,omitempty"`
	CreatedBy      string          `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// CapitalMovement is one fund movement (receipt/disbursement/payment/settlement).
type CapitalMovement struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ContractID     string    `json:"contract_id"`
	MovementType   string    `json:"movement_type"`
	AmountMinor    int64     `json:"amount_minor"`
	CurrencyCode   string    `json:"currency_code"`
	MovementDate   string    `json:"movement_date"`
	Note           string    `json:"note,omitempty"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Status         string    `json:"status"`
	WorkflowCaseID *string   `json:"workflow_case_id,omitempty"`
	JournalEntryID *string   `json:"journal_entry_id,omitempty"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CapitalRepository persists capital domain data.
type CapitalRepository struct {
	db *sql.DB
}

func NewCapitalRepository(db *sql.DB) *CapitalRepository {
	return &CapitalRepository{db: db}
}

// ── Fund types ──

// ListFundTypes returns fund types ordered by code.
func (r *CapitalRepository) ListFundTypes(ctx context.Context, tenantID string, includeInactive bool) ([]FundType, error) {
	active := " AND is_active"
	if includeInactive {
		active = ""
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM cfc_fund_types WHERE tenant_id IN ('', $1)`+active+` ORDER BY code`, tenantID)
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

// GetFundTypeByCode loads one fund type by business code.
func (r *CapitalRepository) GetFundTypeByCode(ctx context.Context, tenantID, code string) (*FundType, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM cfc_fund_types WHERE tenant_id IN ('', $1) AND code = $2`, tenantID, code)
	var t FundType
	err := row.Scan(&t.ID, &t.TenantID, &t.Code, &t.Name, &t.IsActive, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateFundType inserts one fund type.
func (r *CapitalRepository) CreateFundType(ctx context.Context, t *FundType) (*FundType, error) {
	if t.ID == "" {
		t.ID = NewCapitalID("cfcft")
	}
	if t.TenantID == "" {
		t.TenantID = ""
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO cfc_fund_types (id, tenant_id, code, name, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING created_at, updated_at`,
		t.ID, t.TenantID, t.Code, t.Name, t.IsActive, t.CreatedBy)
	if err := row.Scan(&t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateFundType updates name/is_active by id.
func (r *CapitalRepository) UpdateFundType(ctx context.Context, t *FundType) (*FundType, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE cfc_fund_types SET name = $3, is_active = $4, updated_at = now(), version = version + 1
		WHERE tenant_id IN ('', $1) AND id = $2 RETURNING created_at, updated_at`,
		t.TenantID, t.ID, t.Name, t.IsActive)
	if err := row.Scan(&t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

// SetFundTypeActive toggles the soft-delete flag.
func (r *CapitalRepository) SetFundTypeActive(ctx context.Context, tenantID, id string, active bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE cfc_fund_types SET is_active = $3, updated_at = now(), version = version + 1
		WHERE tenant_id IN ('', $1) AND id = $2`, tenantID, id, active)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("fund type not found")
	}
	return nil
}

// ── Products ──

// ListProducts returns fund products ordered by code.
func (r *CapitalRepository) ListProducts(ctx context.Context, tenantID string, includeInactive bool) ([]CapitalProduct, error) {
	active := " AND is_active"
	if includeInactive {
		active = ""
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, code, name, fund_type_code, term_months, interest_rate, currency_code,
		       is_active, COALESCE(created_by,''), created_at, updated_at
		FROM cfc_products WHERE tenant_id = $1`+active+` ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CapitalProduct{}
	for rows.Next() {
		var p CapitalProduct
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.FundTypeCode, &p.TermMonths,
			&p.InterestRate, &p.CurrencyCode, &p.IsActive, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProductByCode loads one product by code.
func (r *CapitalRepository) GetProductByCode(ctx context.Context, tenantID, code string) (*CapitalProduct, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, code, name, fund_type_code, term_months, interest_rate, currency_code,
		       is_active, COALESCE(created_by,''), created_at, updated_at
		FROM cfc_products WHERE tenant_id = $1 AND code = $2`, tenantID, code)
	var p CapitalProduct
	err := row.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.FundTypeCode, &p.TermMonths,
		&p.InterestRate, &p.CurrencyCode, &p.IsActive, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertProduct creates or updates a product by (tenant, code).
func (r *CapitalRepository) UpsertProduct(ctx context.Context, p *CapitalProduct) (*CapitalProduct, error) {
	if p.ID == "" {
		p.ID = NewCapitalID("cfcprd")
	}
	if p.CurrencyCode == "" {
		p.CurrencyCode = "VND"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO cfc_products (id, tenant_id, code, name, fund_type_code, term_months,
			interest_rate, currency_code, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (tenant_id, code) DO UPDATE SET name = EXCLUDED.name,
			fund_type_code = EXCLUDED.fund_type_code, term_months = EXCLUDED.term_months,
			interest_rate = EXCLUDED.interest_rate, currency_code = EXCLUDED.currency_code,
			is_active = EXCLUDED.is_active, updated_at = now(), version = cfc_products.version + 1
		RETURNING created_at, updated_at`,
		p.ID, p.TenantID, p.Code, p.Name, p.FundTypeCode, p.TermMonths,
		p.InterestRate, p.CurrencyCode, p.IsActive, p.CreatedBy)
	if err := row.Scan(&p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	return p, nil
}

// ── Contracts ──

// ListContractsParams carries the parsed list query for fund contracts.
type ListContractsParams struct {
	TenantID string
	OrgCodes []string
	Status   string
	Q        string
	Sort     string
	Order    string
	Page     int // 0-based row offset * size supplied by the handler
	Size     int
}

// contractSortCol maps the FE sort param to a whitelisted column.
func contractSortCol(sort string) string {
	switch sort {
	case "contract_code":
		return "contract_code"
	case "contract_date":
		return "contract_date"
	case "created_at":
		return "created_at"
	default:
		return "contract_date"
	}
}

func listCapOrder(order string) string {
	if order == "desc" {
		return "DESC"
	}
	return "ASC"
}

const contractColumns = `id, tenant_id, contract_code, fund_type_code, COALESCE(product_code,''),
	       counterparty_code, contract_date::text, COALESCE(maturity_date::text,''),
	       amount_minor, interest_rate, currency_code, status, COALESCE(org_code,''),
	       workflow_case_id::text, journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at`

func scanContract(row interface {
	Scan(dest ...any) error
}) (*CapitalContract, error) {
	var c CapitalContract
	var caseID, entryID sql.NullString
	if err := row.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.FundTypeCode, &c.ProductCode,
		&c.CounterpartyCode, &c.ContractDate, &c.MaturityDate, &c.AmountMinor, &c.InterestRate,
		&c.CurrencyCode, &c.Status, &c.OrgCode, &caseID, &entryID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
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

// ListContracts returns a paged slice of fund contracts filtered by status,
// org scope and q (contract_code / counterparty_code / fund_type_code ILIKE).
func (r *CapitalRepository) ListContracts(ctx context.Context, params ListContractsParams) ([]CapitalContract, int, error) {
	where := []string{"tenant_id = $1"}
	args := []any{params.TenantID}
	if len(params.OrgCodes) > 0 {
		args = append(args, params.OrgCodes)
		where = append(where, fmt.Sprintf("org_code = ANY($%d::text[])", len(args)))
	}
	if params.Status != "" {
		args = append(args, params.Status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	if params.Q != "" {
		args = append(args, "%"+params.Q+"%")
		where = append(where, fmt.Sprintf(
			"(contract_code ILIKE $%d OR counterparty_code ILIKE $%d OR fund_type_code ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM cfc_contracts WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	size := params.Size
	if size < 1 || size > 100 {
		size = 100
	}
	offset := params.Page
	if offset < 0 {
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM cfc_contracts
		WHERE %s
		ORDER BY %s %s LIMIT $%d OFFSET $%d`,
		contractColumns, wc, contractSortCol(params.Sort), listCapOrder(params.Order), len(args)+1, len(args)+2),
		append(args, size, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []CapitalContract{}
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

// CreateContract inserts a fund contract with the supplied status.
func (r *CapitalRepository) CreateContract(ctx context.Context, c *CapitalContract) (*CapitalContract, error) {
	if c.ID == "" {
		c.ID = NewCapitalID("cfc")
	}
	if c.Status == "" {
		c.Status = "ACTIVE"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO cfc_contracts (id, tenant_id, contract_code, fund_type_code, product_code,
			counterparty_code, contract_date, maturity_date, amount_minor, interest_rate,
			currency_code, status, org_code, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING created_at, updated_at`,
		c.ID, c.TenantID, c.ContractCode, c.FundTypeCode, nullStringCap(c.ProductCode),
		c.CounterpartyCode, c.ContractDate, nullStringCap(c.MaturityDate), c.AmountMinor,
		c.InterestRate, c.CurrencyCode, c.Status, nullStringCap(c.OrgCode), c.CreatedBy)
	if err := row.Scan(&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return c, nil
}

// GetContractByID loads one contract.
func (r *CapitalRepository) GetContractByID(ctx context.Context, tenantID, id string) (*CapitalContract, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+contractColumns+` FROM cfc_contracts WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	c, err := scanContract(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// SetContractCase stamps the workflow case on a staged contract.
func (r *CapitalRepository) SetContractCase(ctx context.Context, tenantID, id, caseID, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_contracts SET workflow_case_id = NULLIF($3,'')::uuid, updated_by = $4,
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, caseID, actor)
	return err
}

// UpdateContractStatus moves a contract between lifecycle states.
func (r *CapitalRepository) UpdateContractStatus(ctx context.Context, tenantID, id, status, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_contracts SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status, actor)
	return err
}

// AmendmentFields carries the whitelisted contract fields an amendment may change.
type AmendmentFields struct {
	AmountMinor  *int64
	InterestRate *float64
	ContractDate *string
	MaturityDate *string
	Note         string
}

// ApplyContractAmendment updates whitelisted fields on the contract.
func (r *CapitalRepository) ApplyContractAmendment(ctx context.Context, tenantID, id string, f AmendmentFields, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_contracts SET
			amount_minor = COALESCE($3, amount_minor),
			interest_rate = COALESCE($4, interest_rate),
			contract_date = COALESCE($5::date, contract_date),
			maturity_date = COALESCE($6::date, maturity_date),
			updated_by = $7, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, f.AmountMinor, f.InterestRate, f.ContractDate, f.MaturityDate, actor)
	return err
}

// ── Amendments ──

const amendmentColumns = `id, tenant_id, contract_id, status, payload, COALESCE(reason,''),
	workflow_case_id::text, COALESCE(submitted_by,''), submitted_at, COALESCE(created_by,''), created_at, updated_at`

func scanAmendment(row interface {
	Scan(dest ...any) error
}) (*ContractAmendment, error) {
	var a ContractAmendment
	var caseID sql.NullString
	var submittedAt sql.NullTime
	if err := row.Scan(&a.ID, &a.TenantID, &a.ContractID, &a.Status, &a.Payload, &a.Reason,
		&caseID, &a.SubmittedBy, &submittedAt, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	if caseID.Valid {
		a.WorkflowCaseID = &caseID.String
	}
	if submittedAt.Valid {
		a.SubmittedAt = &submittedAt.Time
	}
	return &a, nil
}

// CreateAmendment inserts one staged amendment.
func (r *CapitalRepository) CreateAmendment(ctx context.Context, a *ContractAmendment) (*ContractAmendment, error) {
	if a.ID == "" {
		a.ID = NewCapitalID("cfcadj")
	}
	if len(a.Payload) == 0 {
		a.Payload = json.RawMessage(`{}`)
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO cfc_contract_amendments (id, tenant_id, contract_id, status, payload, reason, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING created_at, updated_at`,
		a.ID, a.TenantID, a.ContractID, a.Status, a.Payload, a.Reason, a.CreatedBy)
	if err := row.Scan(&a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	return a, nil
}

// GetAmendmentByID loads one amendment.
func (r *CapitalRepository) GetAmendmentByID(ctx context.Context, tenantID, id string) (*ContractAmendment, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+amendmentColumns+` FROM cfc_contract_amendments WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	a, err := scanAmendment(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListAmendmentsByContract returns amendments of one contract, newest first.
func (r *CapitalRepository) ListAmendmentsByContract(ctx context.Context, tenantID, contractID string) ([]ContractAmendment, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+amendmentColumns+`
		FROM cfc_contract_amendments WHERE tenant_id = $1 AND contract_id = $2 ORDER BY created_at DESC`, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContractAmendment{}
	for rows.Next() {
		a, err := scanAmendment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// SetAmendmentCase stamps the workflow case + SUBMITTED state.
func (r *CapitalRepository) SetAmendmentCase(ctx context.Context, tenantID, id, caseID, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_contract_amendments SET workflow_case_id = NULLIF($3,'')::uuid, status = 'SUBMITTED',
			submitted_by = $4, submitted_at = now(), updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, caseID, actor)
	return err
}

// SetAmendmentStatus moves the amendment between lifecycle states.
func (r *CapitalRepository) SetAmendmentStatus(ctx context.Context, tenantID, id, status, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_contract_amendments SET status = $3, updated_by = $4, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status, actor)
	return err
}

// ── Movements ──

const movementColumns = `id, tenant_id, contract_id, movement_type, amount_minor, currency_code,
	movement_date::text, COALESCE(note,''), COALESCE(idempotency_key,''), status, workflow_case_id::text,
	journal_entry_id::text, COALESCE(created_by,''), created_at, updated_at`

func scanMovement(row interface {
	Scan(dest ...any) error
}) (*CapitalMovement, error) {
	var m CapitalMovement
	var caseID, entryID sql.NullString
	if err := row.Scan(&m.ID, &m.TenantID, &m.ContractID, &m.MovementType, &m.AmountMinor, &m.CurrencyCode,
		&m.MovementDate, &m.Note, &m.IdempotencyKey, &m.Status, &caseID, &entryID,
		&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
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

// CreateMovement inserts one staged movement.
func (r *CapitalRepository) CreateMovement(ctx context.Context, m *CapitalMovement) (*CapitalMovement, error) {
	if m.ID == "" {
		m.ID = NewCapitalID("cfcmv")
	}
	if m.Status == "" {
		m.Status = "DRAFT"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO cfc_movements (id, tenant_id, contract_id, movement_type, amount_minor,
			currency_code, movement_date, note, idempotency_key, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11)
		RETURNING created_at, updated_at`,
		m.ID, m.TenantID, m.ContractID, m.MovementType, m.AmountMinor, m.CurrencyCode,
		m.MovementDate, m.Note, m.IdempotencyKey, m.Status, m.CreatedBy)
	if err := row.Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return m, nil
}

// GetMovementByID loads one movement.
func (r *CapitalRepository) GetMovementByID(ctx context.Context, tenantID, id string) (*CapitalMovement, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+movementColumns+` FROM cfc_movements WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	m, err := scanMovement(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ListMovementsByContract returns movements of one contract, newest first.
func (r *CapitalRepository) ListMovementsByContract(ctx context.Context, tenantID, contractID string) ([]CapitalMovement, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+movementColumns+`
		FROM cfc_movements WHERE tenant_id = $1 AND contract_id = $2 ORDER BY created_at DESC`, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CapitalMovement{}
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// SetMovementCase stamps the workflow case + SUBMITTED state.
func (r *CapitalRepository) SetMovementCase(ctx context.Context, tenantID, id, caseID, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_movements SET workflow_case_id = NULLIF($3,'')::uuid, status = 'SUBMITTED',
			updated_at = now(), version = version + 1 WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, caseID, actor)
	return err
}

// SetMovementJournal stamps the posting result.
func (r *CapitalRepository) SetMovementJournal(ctx context.Context, tenantID, id, status, journalEntryID, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_movements SET status = $3, journal_entry_id = NULLIF($4,'')::uuid,
			updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status, journalEntryID)
	return err
}

// SetMovementStatus moves the movement between lifecycle states.
func (r *CapitalRepository) SetMovementStatus(ctx context.Context, tenantID, id, status, actor string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE cfc_movements SET status = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2`, tenantID, id, status)
	return err
}

// PostedMovementTotals returns Σ POSTED receipts and Σ POSTED outflows for one
// contract (used for the availability guard).
func (r *CapitalRepository) PostedMovementTotals(ctx context.Context, tenantID, contractID string) (int64, int64, error) {
	var receipts, outflows sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN movement_type = 'RECEIPT' THEN amount_minor END), 0),
			COALESCE(SUM(CASE WHEN movement_type IN ('DISBURSEMENT','PAYMENT','SETTLEMENT') THEN amount_minor END), 0)
		FROM cfc_movements WHERE tenant_id = $1 AND contract_id = $2 AND status = 'POSTED'`,
		tenantID, contractID).Scan(&receipts, &outflows)
	if err != nil {
		return 0, 0, err
	}
	return receipts.Int64, outflows.Int64, nil
}

// NewCapitalID generates a prefixed random id.
func NewCapitalID(prefix string) string {
	var b [16]byte
	if _, err := cryptoRandCap.Read(b[:]); err != nil {
		panic("capital id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

// ── Reports (W4c) ──

// FundSourceStatementRow is one sổ nguồn vốn row.
type FundSourceStatementRow struct {
	ContractCode     string  `json:"contract_code"`
	FundTypeCode     string  `json:"fund_type_code"`
	CounterpartyCode string  `json:"counterparty_code"`
	ContractDate     string  `json:"contract_date"`
	MaturityDate     string  `json:"maturity_date,omitempty"`
	AmountMinor      int64   `json:"amount_minor"`
	InterestRate     float64 `json:"interest_rate"`
	CurrencyCode     string  `json:"currency_code"`
	Status           string  `json:"status"`
}

// FundSourceTxnRow is one giao dịch nguồn vốn row.
type FundSourceTxnRow struct {
	MovementDate string `json:"movement_date"`
	ContractCode string `json:"contract_code"`
	MovementType string `json:"movement_type"`
	AmountMinor  int64  `json:"amount_minor"`
	CurrencyCode string `json:"currency_code"`
	Note         string `json:"note,omitempty"`
	Status       string `json:"status"`
}

// FundSourceStatement lists fund contracts with contract_date in [from, to].
func (r *CapitalRepository) FundSourceStatement(ctx context.Context, tenantID, fromDate, toDate, status string) ([]FundSourceStatementRow, error) {
	where := []string{"tenant_id = $1"}
	args := []any{tenantID}
	if fromDate != "" {
		args = append(args, fromDate)
		where = append(where, fmt.Sprintf("contract_date >= $%d::date", len(args)))
	}
	if toDate != "" {
		args = append(args, toDate)
		where = append(where, fmt.Sprintf("contract_date <= $%d::date", len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT contract_code, fund_type_code, counterparty_code, contract_date::text,
		       COALESCE(maturity_date::text,''), amount_minor, interest_rate, currency_code, status
		FROM cfc_contracts WHERE `+strings.Join(where, " AND ")+`
		ORDER BY contract_date DESC, contract_code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FundSourceStatementRow{}
	for rows.Next() {
		var x FundSourceStatementRow
		if err := rows.Scan(&x.ContractCode, &x.FundTypeCode, &x.CounterpartyCode, &x.ContractDate,
			&x.MaturityDate, &x.AmountMinor, &x.InterestRate, &x.CurrencyCode, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// FundSourceTransactions lists fund movements with movement_date in [from, to].
func (r *CapitalRepository) FundSourceTransactions(ctx context.Context, tenantID, fromDate, toDate, movementType string) ([]FundSourceTxnRow, error) {
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
	if movementType != "" {
		args = append(args, movementType)
		where = append(where, fmt.Sprintf("m.movement_type = $%d::text", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.movement_date::text, c.contract_code, m.movement_type, m.amount_minor,
		       m.currency_code, COALESCE(m.note,''), m.status
		FROM cfc_movements m JOIN cfc_contracts c ON c.id = m.contract_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY m.movement_date DESC, c.contract_code LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FundSourceTxnRow{}
	for rows.Next() {
		var x FundSourceTxnRow
		if err := rows.Scan(&x.MovementDate, &x.ContractCode, &x.MovementType, &x.AmountMinor,
			&x.CurrencyCode, &x.Note, &x.Status); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func nullStringCap(s string) any {
	if s == "" {
		return nil
	}
	return s
}
