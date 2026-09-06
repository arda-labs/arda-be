package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

// Sentinel errors mapped to HTTP statuses by the service layer.
var (
	ErrNotFound = errors.New("lnm: record not found")
	ErrConflict = errors.New("lnm: code conflict")
)

// NewID generates a prefixed random-hex identifier.
func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure loan id generation failed: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

type LoanRepository struct {
	db *sql.DB
}

func NewLoanRepository(db *sql.DB) *LoanRepository {
	return &LoanRepository{db: db}
}

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return err
}

// ── Contracts ──

const contractColumns = `id, tenant_id, contract_code, contract_no, customer_code, employee_code,
	contract_type_code, product_code, interest_rate, interest_rate_type, purpose_code, industry_code,
	loan_method_code, contract_date::text, loan_term, term_unit, maturity_date::text,
	interest_schedule_day, loan_amt, interest_payment_freq, principal_payment_freq,
	interest_payment_method, principal_payment_method, status, workflow_case_id, created_by, created_at, updated_at`

func scanContract(s interface{ Scan(...any) error }) (domain.Contract, error) {
	var c domain.Contract
	err := s.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.ContractNo, &c.CustomerCode, &c.EmployeeCode,
		&c.ContractTypeCode, &c.ProductCode, &c.InterestRate, &c.InterestRateType, &c.PurposeCode, &c.IndustryCode,
		&c.LoanMethodCode, &c.ContractDate, &c.LoanTerm, &c.TermUnit, &c.MaturityDate,
		&c.InterestScheduleDay, &c.LoanAmt, &c.InterestPaymentFreq, &c.PrincipalPaymentFreq,
		&c.InterestPaymentMethod, &c.PrincipalPaymentMethod, &c.Status, &c.WorkflowCaseID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (r *LoanRepository) ListContracts(ctx context.Context, tenantID, status, q string) ([]domain.Contract, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+contractColumns+`
		FROM lnm_contracts
		WHERE tenant_id = $1
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR contract_code ILIKE '%' || $3 || '%' OR contract_no ILIKE '%' || $3 || '%' OR customer_code ILIKE '%' || $3 || '%')
		ORDER BY created_at DESC
		LIMIT 500`, tenantID, status, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Contract{}
	for rows.Next() {
		item, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *LoanRepository) GetContract(ctx context.Context, tenantID, id string) (domain.Contract, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+contractColumns+` FROM lnm_contracts WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	item, err := scanContract(row)
	return item, mapNoRows(err)
}

func (r *LoanRepository) CreateContract(ctx context.Context, c *domain.Contract) (*domain.Contract, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_contracts (id, tenant_id, contract_code, contract_no, customer_code, employee_code,
			contract_type_code, product_code, interest_rate, interest_rate_type, purpose_code, industry_code,
			loan_method_code, contract_date, loan_term, term_unit, maturity_date, interest_schedule_day,
			loan_amt, interest_payment_freq, principal_payment_freq, interest_payment_method,
			principal_payment_method, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::date,$15,$16,$17::date,$18,$19,$20,$21,$22,$23,$24,$25)
		ON CONFLICT (tenant_id, contract_code) DO NOTHING
		RETURNING `+contractColumns,
		c.ID, c.TenantID, c.ContractCode, c.ContractNo, c.CustomerCode, c.EmployeeCode,
		c.ContractTypeCode, c.ProductCode, c.InterestRate, c.InterestRateType, c.PurposeCode, c.IndustryCode,
		c.LoanMethodCode, c.ContractDate, c.LoanTerm, c.TermUnit, c.MaturityDate, c.InterestScheduleDay,
		c.LoanAmt, c.InterestPaymentFreq, c.PrincipalPaymentFreq, c.InterestPaymentMethod,
		c.PrincipalPaymentMethod, c.Status, c.CreatedBy)
	out, err := scanContract(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrConflict)
	}
	return &out, err
}

func (r *LoanRepository) UpdateContractStatus(ctx context.Context, tenantID, id, status string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE lnm_contracts SET status = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2`, tenantID, id, status)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}

func (r *LoanRepository) SetContractWorkflowCase(ctx context.Context, tenantID, id, caseID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE lnm_contracts SET workflow_case_id = $3, status = 'PENDING', updated_at = now() WHERE tenant_id = $1 AND id = $2`, tenantID, id, caseID)
	return err
}

// ── Agreements (disbursements) ──

const agreementColumns = `id, tenant_id, contract_code, agreement_code, disburse_date::text, disburse_amt,
	interest_rate, over_interest_rate, loan_term, term_unit, maturity_date::text, debt_group_code,
	interest_payment_freq, principal_payment_freq, outstanding_amt, coln_principal_amt, coln_interest_amt,
	provision_amt, acc_classification, status, created_by, created_at, updated_at`

func scanAgreement(s interface{ Scan(...any) error }) (domain.Agreement, error) {
	var a domain.Agreement
	err := s.Scan(&a.ID, &a.TenantID, &a.ContractCode, &a.AgreementCode, &a.DisburseDate, &a.DisburseAmt,
		&a.InterestRate, &a.OverInterestRate, &a.LoanTerm, &a.TermUnit, &a.MaturityDate, &a.DebtGroupCode,
		&a.InterestPaymentFreq, &a.PrincipalPaymentFreq, &a.OutstandingAmt, &a.ColnPrincipalAmt, &a.ColnInterestAmt,
		&a.ProvisionAmt, &a.AccClassification, &a.Status, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (r *LoanRepository) ListAgreements(ctx context.Context, tenantID, contractCode string) ([]domain.Agreement, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+agreementColumns+`
		FROM lnm_agreements
		WHERE tenant_id = $1 AND ($2 = '' OR contract_code = $2)
		ORDER BY disburse_date DESC, agreement_code`, tenantID, contractCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Agreement{}
	for rows.Next() {
		item, err := scanAgreement(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *LoanRepository) GetAgreement(ctx context.Context, tenantID, id string) (domain.Agreement, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+agreementColumns+` FROM lnm_agreements WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	item, err := scanAgreement(row)
	return item, mapNoRows(err)
}

func (r *LoanRepository) GetAgreementByCode(ctx context.Context, tenantID, agreementCode string) (domain.Agreement, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+agreementColumns+` FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, agreementCode)
	item, err := scanAgreement(row)
	return item, mapNoRows(err)
}

func (r *LoanRepository) CreateAgreement(ctx context.Context, a *domain.Agreement) (*domain.Agreement, error) {
	if a.Status == "" {
		a.Status = "ACTIVE"
	}
	if a.DebtGroupCode == "" {
		a.DebtGroupCode = "GROUP_1"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_agreements (id, tenant_id, contract_code, agreement_code, disburse_date, disburse_amt,
			interest_rate, over_interest_rate, loan_term, term_unit, maturity_date, debt_group_code,
			interest_payment_freq, principal_payment_freq, outstanding_amt, acc_classification, created_by)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11::date,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (tenant_id, agreement_code) DO NOTHING
		RETURNING `+agreementColumns,
		a.ID, a.TenantID, a.ContractCode, a.AgreementCode, a.DisburseDate, a.DisburseAmt,
		a.InterestRate, a.OverInterestRate, a.LoanTerm, a.TermUnit, a.MaturityDate, a.DebtGroupCode,
		a.InterestPaymentFreq, a.PrincipalPaymentFreq, a.OutstandingAmt, a.AccClassification, a.CreatedBy)
	out, err := scanAgreement(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrConflict)
	}
	return &out, err
}

// ── Repay plans ──

func (r *LoanRepository) ListRepayPlans(ctx context.Context, tenantID, contractCode, agreementCode string) ([]domain.RepayPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, contract_code, agreement_code, plan_no, term_no, from_date::text, to_date::text,
		       interest_rate, plan_principal_amt, plan_interest_amt, coln_principal_amt, coln_interest_amt,
		       is_active, created_at, updated_at
		FROM lnm_repay_plans
		WHERE tenant_id = $1 AND ($2 = '' OR contract_code = $2) AND ($3 = '' OR agreement_code = $3)
		ORDER BY agreement_code NULLS LAST, term_no`, tenantID, contractCode, agreementCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.RepayPlan{}
	for rows.Next() {
		var p domain.RepayPlan
		if err := rows.Scan(&p.ID, &p.TenantID, &p.ContractCode, &p.AgreementCode, &p.PlanNo, &p.TermNo,
			&p.FromDate, &p.ToDate, &p.InterestRate, &p.PlanPrincipalAmt, &p.PlanInterestAmt,
			&p.ColnPrincipalAmt, &p.ColnInterestAmt, &p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// ReplaceRepayPlans rewrites the active schedule for one agreement
// (restructure / plan generation semantics — mirrors EPAS plan regeneration).
func (r *LoanRepository) ReplaceRepayPlans(ctx context.Context, tenantID, agreementCode string, plans []domain.RepayPlan) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM lnm_repay_plans WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, agreementCode); err != nil {
		return err
	}
	for i := range plans {
		p := &plans[i]
		if p.ID == "" {
			p.ID = NewID("plan")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO lnm_repay_plans (id, tenant_id, contract_code, agreement_code, plan_no, term_no,
				from_date, to_date, interest_rate, plan_principal_amt, plan_interest_amt, coln_principal_amt, coln_interest_amt, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,$7::date,$8::date,$9,$10,$11,$12,$13,TRUE)`,
			p.ID, tenantID, p.ContractCode, p.AgreementCode, p.PlanNo, p.TermNo,
			p.FromDate, p.ToDate, p.InterestRate, p.PlanPrincipalAmt, p.PlanInterestAmt,
			p.ColnPrincipalAmt, p.ColnInterestAmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ── Mortgages / collaterals ──

func (r *LoanRepository) ListMortgages(ctx context.Context, tenantID, q string) ([]domain.Mortgage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, mortgage_code, mortgage_no, customer_code, mortgage_date::text,
		       notarization_date::text, registration_date::text, expire_date::text, status, description,
		       created_at, updated_at
		FROM lnm_mortgages
		WHERE tenant_id = $1 AND ($2 = '' OR mortgage_code ILIKE '%' || $2 || '%' OR customer_code ILIKE '%' || $2 || '%')
		ORDER BY created_at DESC LIMIT 500`, tenantID, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Mortgage{}
	for rows.Next() {
		var m domain.Mortgage
		var desc sql.NullString
		if err := rows.Scan(&m.ID, &m.TenantID, &m.MortgageCode, &m.MortgageNo, &m.CustomerCode, &m.MortgageDate,
			&m.NotarizationDate, &m.RegistrationDate, &m.ExpireDate, &m.Status, &desc, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		if desc.Valid {
			m.Description = &desc.String
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateMortgage(ctx context.Context, m *domain.Mortgage) (*domain.Mortgage, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_mortgages (id, tenant_id, mortgage_code, mortgage_no, customer_code, mortgage_date,
			notarization_date, registration_date, expire_date, status, description, created_by)
		VALUES ($1,$2,$3,$4,$5,$6::date,$7::date,$8::date,$9::date,$10,$11,$12)
		ON CONFLICT (tenant_id, mortgage_code) DO NOTHING
		RETURNING id, tenant_id, mortgage_code, mortgage_no, customer_code, mortgage_date::text,
		          notarization_date::text, registration_date::text, expire_date::text, status, description,
		          created_at, updated_at`,
		m.ID, m.TenantID, m.MortgageCode, m.MortgageNo, m.CustomerCode, m.MortgageDate,
		m.NotarizationDate, m.RegistrationDate, m.ExpireDate, m.Status, m.Description, m.CreatedAt)
	out := &domain.Mortgage{}
	var desc sql.NullString
	if err := row.Scan(&out.ID, &out.TenantID, &out.MortgageCode, &out.MortgageNo, &out.CustomerCode, &out.MortgageDate,
		&out.NotarizationDate, &out.RegistrationDate, &out.ExpireDate, &out.Status, &desc, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrConflict)
		}
		return nil, err
	}
	if desc.Valid {
		out.Description = &desc.String
	}
	return out, nil
}

func (r *LoanRepository) ListCollaterals(ctx context.Context, tenantID, mortgageCode, q string) ([]domain.Collateral, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, coll_code, coll_name, coll_type_code, mortgage_code, owner_cif_code, owner_name,
		       coll_address, quantity, unit_price, coll_value, coll_use_value, valuation_date::text, status,
		       created_at, updated_at
		FROM lnm_collaterals
		WHERE tenant_id = $1
		  AND ($2 = '' OR mortgage_code = $2)
		  AND ($3 = '' OR coll_code ILIKE '%' || $3 || '%' OR coll_name ILIKE '%' || $3 || '%')
		ORDER BY created_at DESC LIMIT 500`, tenantID, mortgageCode, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Collateral{}
	for rows.Next() {
		var c domain.Collateral
		if err := rows.Scan(&c.ID, &c.TenantID, &c.CollCode, &c.CollName, &c.CollTypeCode, &c.MortgageCode,
			&c.OwnerCifCode, &c.OwnerName, &c.CollAddress, &c.Quantity, &c.UnitPrice, &c.CollValue,
			&c.CollUseValue, &c.ValuationDate, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateCollateral(ctx context.Context, c *domain.Collateral) (*domain.Collateral, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_collaterals (id, tenant_id, coll_code, coll_name, coll_type_code, mortgage_code,
			owner_cif_code, owner_name, coll_address, quantity, unit_price, coll_value, coll_use_value,
			valuation_date, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::date,$15,$16)
		ON CONFLICT (tenant_id, coll_code) DO NOTHING
		RETURNING id, tenant_id, coll_code, coll_name, coll_type_code, mortgage_code, owner_cif_code, owner_name,
		          coll_address, quantity, unit_price, coll_value, coll_use_value, valuation_date::text, status,
		          created_at, updated_at`,
		c.ID, c.TenantID, c.CollCode, c.CollName, c.CollTypeCode, c.MortgageCode, c.OwnerCifCode, c.OwnerName,
		c.CollAddress, c.Quantity, c.UnitPrice, c.CollValue, c.CollUseValue, c.ValuationDate, c.Status, c.CreatedAt)
	out, err := scanCollateral(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrConflict)
	}
	return &out, err
}

func scanCollateral(s interface{ Scan(...any) error }) (domain.Collateral, error) {
	var c domain.Collateral
	err := s.Scan(&c.ID, &c.TenantID, &c.CollCode, &c.CollName, &c.CollTypeCode, &c.MortgageCode, &c.OwnerCifCode,
		&c.OwnerName, &c.CollAddress, &c.Quantity, &c.UnitPrice, &c.CollValue, &c.CollUseValue,
		&c.ValuationDate, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (r *LoanRepository) ListContractCollaterals(ctx context.Context, tenantID, contractCode string) ([]domain.ContractCollateral, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, contract_code, coll_code, coll_value, created_at, updated_at
		FROM lnm_contract_collaterals
		WHERE tenant_id = $1 AND ($2 = '' OR contract_code = $2)
		ORDER BY coll_code`, tenantID, contractCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.ContractCollateral{}
	for rows.Next() {
		var cc domain.ContractCollateral
		if err := rows.Scan(&cc.ID, &cc.TenantID, &cc.ContractCode, &cc.CollCode, &cc.CollValue, &cc.CreatedAt, &cc.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, cc)
	}
	return items, rows.Err()
}

func (r *LoanRepository) AttachContractCollateral(ctx context.Context, cc *domain.ContractCollateral) (*domain.ContractCollateral, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_contract_collaterals (id, tenant_id, contract_code, coll_code, coll_value)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (tenant_id, contract_code, coll_code) DO UPDATE SET coll_value = EXCLUDED.coll_value, updated_at = now()
		RETURNING id, tenant_id, contract_code, coll_code, coll_value, created_at, updated_at`,
		cc.ID, cc.TenantID, cc.ContractCode, cc.CollCode, cc.CollValue)
	out := &domain.ContractCollateral{}
	err := row.Scan(&out.ID, &out.TenantID, &out.ContractCode, &out.CollCode, &out.CollValue, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

// ── Adjustments (uniform shape, one table per kind) ──

// AdjustmentTables maps each registered flow kind to its table. Kind names
// come from the shared list in libs/go/arda-grpc/client/loan — callers must
// validate with loan.IsValidKind before reaching this map.
var AdjustmentTables = map[string]string{
	"debt-change":        "lnm_debt_changes",
	"rate-change":        "lnm_rate_changes",
	"restructure":        "lnm_restructures",
	"waiver":             "lnm_waivers",
	"writeoff":           "lnm_writeoffs",
	"recovery":           "lnm_recoveries",
	"fund-check":         "lnm_fund_checks",
	"revenue-allocation": "lnm_revenue_allocations",
	"vfu-fee-allocation": "lnm_vfu_fee_allocations",
	"off-balance-export": "lnm_off_balance_exports",
}

const adjustmentColumns = `id, tenant_id, contract_code, agreement_code, effective_date::text, amount,
	payload, status, workflow_case_id, decision_note, decided_by, created_by, created_at, updated_at`

func scanAdjustment(s interface{ Scan(...any) error }) (domain.Adjustment, error) {
	var a domain.Adjustment
	var agreementCode, effectiveDate sql.NullString
	var amount sql.NullFloat64
	var payload []byte
	var caseID, note, decidedBy sql.NullString
	err := s.Scan(&a.ID, &a.TenantID, &a.ContractCode, &agreementCode, &effectiveDate, &amount,
		&payload, &a.Status, &caseID, &note, &decidedBy, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
	if agreementCode.Valid {
		a.AgreementCode = &agreementCode.String
	}
	if effectiveDate.Valid {
		a.EffectiveDate = &effectiveDate.String
	}
	if amount.Valid {
		a.Amount = &amount.Float64
	}
	if len(payload) > 0 && string(payload) != "null" {
		a.Payload = payload
	}
	if caseID.Valid {
		a.WorkflowCaseID = &caseID.String
	}
	if note.Valid {
		a.DecisionNote = &note.String
	}
	if decidedBy.Valid {
		a.DecidedBy = &decidedBy.String
	}
	return a, err
}

func (r *LoanRepository) ListAdjustments(ctx context.Context, table, tenantID, contractCode, status string) ([]domain.Adjustment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+adjustmentColumns+`
		FROM `+table+`
		WHERE tenant_id = $1
		  AND ($2 = '' OR contract_code = $2)
		  AND ($3 = '' OR status = $3)
		ORDER BY created_at DESC LIMIT 500`, tenantID, contractCode, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Adjustment{}
	for rows.Next() {
		item, err := scanAdjustment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *LoanRepository) GetAdjustment(ctx context.Context, table, tenantID, id string) (domain.Adjustment, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+adjustmentColumns+` FROM `+table+` WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	item, err := scanAdjustment(row)
	return item, mapNoRows(err)
}

func (r *LoanRepository) CreateAdjustment(ctx context.Context, table string, a *domain.Adjustment) (*domain.Adjustment, error) {
	if a.Status == "" {
		a.Status = domain.AdjustmentDraft
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO `+table+` (id, tenant_id, contract_code, agreement_code, effective_date, amount, payload, status, created_by)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9)
		RETURNING `+adjustmentColumns,
		a.ID, a.TenantID, a.ContractCode, a.AgreementCode, a.EffectiveDate, a.Amount, nullIfEmpty(a.Payload), a.Status, a.CreatedBy)
	out, err := scanAdjustment(row)
	return &out, err
}

func (r *LoanRepository) SetAdjustmentWorkflowCase(ctx context.Context, table, tenantID, id, caseID string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE `+table+` SET workflow_case_id = $3, status = 'PENDING', updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'DRAFT'`, tenantID, id, caseID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w", ErrNotFound)
	}
	return nil
}

// ResolveAdjustment applies a workflow decision. Approving a debt-change or
// rate-change also applies the side effect onto the target agreement — the
// two flows whose outcome is a plain field update.
func (r *LoanRepository) ResolveAdjustment(ctx context.Context, table, tenantID, id, decision, decidedBy, note string) (domain.Adjustment, error) {
	status := decision
	switch strings.ToUpper(decision) {
	case "APPROVE":
		status = domain.AdjustmentActive
	case "REJECT":
		status = domain.AdjustmentRejected
	case "CANCEL":
		status = domain.AdjustmentCancelled
	default:
		return domain.Adjustment{}, fmt.Errorf("unknown decision %q", decision)
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE `+table+` SET status = $3, decided_by = $4, decision_note = $5, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+adjustmentColumns, tenantID, id, status, decidedBy, note)
	item, err := scanAdjustment(row)
	if err = mapNoRows(err); err != nil {
		return domain.Adjustment{}, err
	}
	if status == domain.AdjustmentActive {
		if err := r.applyAdjustmentSideEffect(ctx, tenantID, table, item); err != nil {
			return domain.Adjustment{}, err
		}
	}
	return item, nil
}

func (r *LoanRepository) applyAdjustmentSideEffect(ctx context.Context, tenantID, table string, item domain.Adjustment) error {
	var payload map[string]any
	if len(item.Payload) > 0 {
		_ = json.Unmarshal(item.Payload, &payload)
	}
	payloadString := func(key string) string {
		if v, ok := payload[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	payloadInt := func(key string) (int, bool) {
		if v, ok := payload[key].(float64); ok {
			return int(v), true
		}
		return 0, false
	}
	switch table {
	case "lnm_debt_changes":
		toGroup := payloadString("to_debt_group_code")
		if item.AgreementCode != nil && *item.AgreementCode != "" && toGroup != "" {
			if _, err := r.db.ExecContext(ctx, `
				UPDATE lnm_agreements SET debt_group_code = $3, updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, toGroup); err != nil {
				return err
			}
		}
	case "lnm_rate_changes":
		newRate := payloadString("new_rate")
		if item.AgreementCode != nil && *item.AgreementCode != "" && newRate != "" {
			if _, err := r.db.ExecContext(ctx, `
				UPDATE lnm_agreements SET interest_rate = $3, updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, newRate); err != nil {
				return err
			}
		}
	case "lnm_restructures":
		// Real restructure: move maturity/term onto the agreement and
		// regenerate an even-principal schedule over the new term count.
		if item.AgreementCode == nil || *item.AgreementCode == "" {
			return nil
		}
		newMaturity := payloadString("new_maturity_date")
		termCount := 0
		if v, ok := payloadInt("new_term"); ok {
			termCount = v
		}
		if newMaturity != "" {
			if _, err := r.db.ExecContext(ctx, `
				UPDATE lnm_agreements SET maturity_date = $3::date, updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, newMaturity); err != nil {
				return err
			}
		}
		if termCount > 0 {
			start := item.EffectiveDate
			if start == nil || *start == "" {
				today := time.Now().Format("2006-01-02")
				start = &today
			}
			if err := r.RegeneratePlans(ctx, tenantID, *item.AgreementCode, termCount, *start); err != nil {
				return err
			}
		}
	case "lnm_waivers":
		// Real waiver: shave unpaid interest across the schedule until the
		// waiver amount (or percent of outstanding interest) is consumed.
		amount := 0.0
		if item.Amount != nil {
			amount = *item.Amount
		} else if pct, ok := payloadInt("waiver_percent"); ok {
			row := r.db.QueryRowContext(ctx, `
				SELECT COALESCE(SUM(plan_interest_amt - coln_interest_amt), 0)
				FROM lnm_repay_plans
				WHERE tenant_id = $1 AND agreement_code = $2 AND is_active AND plan_interest_amt > coln_interest_amt`,
				tenantID, derefAgreement(item))
			var total float64
			if err := row.Scan(&total); err != nil {
				return err
			}
			amount = total * float64(pct) / 100
		}
		if amount > 0 && item.AgreementCode != nil && *item.AgreementCode != "" {
			if _, err := r.ReducePlanInterest(ctx, tenantID, *item.AgreementCode, amount); err != nil {
				return err
			}
		}
	case "lnm_writeoffs":
		// Real write-off: remove the written-off amount from outstanding and
		// close the agreement once nothing is left.
		if item.AgreementCode != nil && *item.AgreementCode != "" && item.Amount != nil {
			if _, err := r.db.ExecContext(ctx, `
				UPDATE lnm_agreements
				SET outstanding_amt = GREATEST(outstanding_amt - $3, 0),
				    status = CASE WHEN GREATEST(outstanding_amt - $3, 0) = 0 THEN 'CLOSED' ELSE status END,
				    updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, *item.Amount); err != nil {
				return err
			}
		}
	case "lnm_recoveries":
		if item.AgreementCode != nil && *item.AgreementCode != "" && item.Amount != nil {
			if _, err := r.db.ExecContext(ctx, `
				UPDATE lnm_agreements SET outstanding_amt = GREATEST(outstanding_amt - $3, 0), coln_principal_amt = coln_principal_amt + $3, updated_at = now()
				WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, *item.AgreementCode, *item.Amount); err != nil {
				return err
			}
		}
	}
	return nil
}

func derefAgreement(item domain.Adjustment) string {
	if item.AgreementCode != nil {
		return *item.AgreementCode
	}
	return ""
}

func nullIfEmpty(payload []byte) any {
	if len(payload) == 0 || strings.TrimSpace(string(payload)) == "" || string(payload) == "null" {
		return nil
	}
	return []byte(payload)
}

// ── Products ──

const productColumns = `id, tenant_id, code, name, product_type, currency_code, interest_rate_code,
	interest_rate, loan_term_from, loan_term_to, term_unit, min_amount, max_amount,
	acc_classification, is_active, description, created_by, created_at, updated_at`

func scanProduct(s interface{ Scan(...any) error }) (domain.LoanProduct, error) {
	var p domain.LoanProduct
	err := s.Scan(&p.ID, &p.TenantID, &p.Code, &p.Name, &p.ProductType, &p.CurrencyCode,
		&p.InterestRateCode, &p.InterestRate, &p.LoanTermFrom, &p.LoanTermTo, &p.TermUnit,
		&p.MinAmount, &p.MaxAmount, &p.AccClassification, &p.IsActive, &p.Description,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (r *LoanRepository) ListProducts(ctx context.Context, tenantID string, includeInactive bool) ([]domain.LoanProduct, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+productColumns+`
		FROM lnm_products
		WHERE tenant_id = $1 AND ($2 OR is_active)
		ORDER BY code`, tenantID, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.LoanProduct{}
	for rows.Next() {
		item, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *LoanRepository) GetProductByCode(ctx context.Context, tenantID, code string) (domain.LoanProduct, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+productColumns+` FROM lnm_products WHERE tenant_id = $1 AND code = $2 AND is_active`, tenantID, code)
	item, err := scanProduct(row)
	return item, mapNoRows(err)
}

func (r *LoanRepository) UpsertProduct(ctx context.Context, p *domain.LoanProduct) (*domain.LoanProduct, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_products (id, tenant_id, code, name, product_type, currency_code, interest_rate_code,
			interest_rate, loan_term_from, loan_term_to, term_unit, min_amount, max_amount,
			acc_classification, is_active, description, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (tenant_id, code) DO UPDATE SET
			name = EXCLUDED.name, product_type = EXCLUDED.product_type, currency_code = EXCLUDED.currency_code,
			interest_rate_code = EXCLUDED.interest_rate_code, interest_rate = EXCLUDED.interest_rate,
			loan_term_from = EXCLUDED.loan_term_from, loan_term_to = EXCLUDED.loan_term_to,
			term_unit = EXCLUDED.term_unit, min_amount = EXCLUDED.min_amount, max_amount = EXCLUDED.max_amount,
			acc_classification = EXCLUDED.acc_classification, is_active = EXCLUDED.is_active,
			description = EXCLUDED.description, updated_at = now()
		RETURNING `+productColumns,
		p.ID, p.TenantID, p.Code, p.Name, p.ProductType, p.CurrencyCode, p.InterestRateCode,
		p.InterestRate, p.LoanTermFrom, p.LoanTermTo, p.TermUnit, p.MinAmount, p.MaxAmount,
		p.AccClassification, p.IsActive, p.Description, p.CreatedBy)
	out, err := scanProduct(row)
	return &out, err
}

// RegeneratePlans rebuilds an even-principal monthly schedule for one
// agreement (restructure semantics): outstanding spread over termCount
// months at the agreement's current rate.
func (r *LoanRepository) RegeneratePlans(ctx context.Context, tenantID, agreementCode string, termCount int, startDate string) error {
	agreement, err := r.GetAgreementByCode(ctx, tenantID, agreementCode)
	if err != nil {
		return err
	}
	outstanding := agreement.OutstandingAmt
	rate := agreement.InterestRate
	perTerm := outstanding / float64(termCount)

	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		start = time.Now().UTC()
	}
	plans := make([]domain.RepayPlan, 0, termCount)
	remaining := outstanding
	for i := 1; i <= termCount; i++ {
		principal := perTerm
		if i == termCount {
			principal = remaining
		}
		from := start.AddDate(0, i-1, 0)
		to := start.AddDate(0, i, 0)
		plans = append(plans, domain.RepayPlan{
			ContractCode:     agreement.ContractCode,
			AgreementCode:    agreement.AgreementCode,
			PlanNo:           1,
			TermNo:           i,
			FromDate:         from.Format("2006-01-02"),
			ToDate:           to.Format("2006-01-02"),
			InterestRate:     rate,
			PlanPrincipalAmt: principal,
			PlanInterestAmt:  remaining * rate / 100 / 12,
		})
		remaining -= principal
	}
	return r.ReplaceRepayPlans(ctx, tenantID, agreementCode, plans)
}

// ReducePlanInterest applies an interest waiver across unpaid schedule rows
// (coln < plan), largest balance first, until the waiver amount is consumed.
func (r *LoanRepository) ReducePlanInterest(ctx context.Context, tenantID, agreementCode string, waiverAmount float64) (float64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_interest_amt - coln_interest_amt
		FROM lnm_repay_plans
		WHERE tenant_id = $1 AND agreement_code = $2 AND is_active
		  AND plan_interest_amt > coln_interest_amt
		ORDER BY to_date`, tenantID, agreementCode)
	if err != nil {
		return 0, err
	}
	type target struct {
		id    string
		due   float64
	}
	targets := []target{}
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.due); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, t)
	}
	rows.Close()

	remaining := waiverAmount
	applied := 0.0
	for _, t := range targets {
		if remaining <= 0 {
			break
		}
		reduce := t.due
		if reduce > remaining {
			reduce = remaining
		}
		if _, err := r.db.ExecContext(ctx, `
			UPDATE lnm_repay_plans SET plan_interest_amt = plan_interest_amt - $3, updated_at = now()
			WHERE tenant_id = $1 AND id = $2`, tenantID, t.id, reduce); err != nil {
			return applied, err
		}
		remaining -= reduce
		applied += reduce
	}
	return applied, nil
}

// ── VFU (ủy thác) ──

func (r *LoanRepository) ListVfuParties(ctx context.Context, tenantID, q string) ([]domain.VfuParty, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, party_code, party_name, party_type, COALESCE(gender_code,''), COALESCE(date_of_birth::text,''),
		       COALESCE(identification_id,''), COALESCE(issue_date::text,''), COALESCE(issue_place,''),
		       COALESCE(mobile_number,''), COALESCE(permanent_address,''), COALESCE(customer_reln_code,''),
		       status, created_by, created_at, updated_at
		FROM lnm_vfu_parties
		WHERE tenant_id = $1 AND ($2 = '' OR party_code ILIKE '%' || $2 || '%' OR party_name ILIKE '%' || $2 || '%')
		ORDER BY party_code LIMIT 500`, tenantID, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.VfuParty{}
	for rows.Next() {
		var p domain.VfuParty
		if err := rows.Scan(&p.ID, &p.TenantID, &p.PartyCode, &p.PartyName, &p.PartyType, &p.GenderCode,
			&p.DateOfBirth, &p.IdentificationID, &p.IssueDate, &p.IssuePlace, &p.MobileNumber,
			&p.PermanentAddress, &p.CustomerRelnCode, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateVfuParty(ctx context.Context, p *domain.VfuParty) (*domain.VfuParty, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_vfu_parties (id, tenant_id, party_code, party_name, party_type, gender_code,
			date_of_birth, identification_id, issue_date, issue_place, mobile_number,
			permanent_address, customer_reln_code, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7::date,$8,$9::date,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (tenant_id, party_code) DO NOTHING
		RETURNING id, tenant_id, party_code, party_name, party_type, COALESCE(gender_code,''), COALESCE(date_of_birth::text,''),
		          COALESCE(identification_id,''), COALESCE(issue_date::text,''), COALESCE(issue_place,''),
		          COALESCE(mobile_number,''), COALESCE(permanent_address,''), COALESCE(customer_reln_code,''),
		          status, created_by, created_at, updated_at`,
		p.ID, p.TenantID, p.PartyCode, p.PartyName, p.PartyType, p.GenderCode,
		p.DateOfBirth, p.IdentificationID, p.IssueDate, p.IssuePlace, p.MobileNumber,
		p.PermanentAddress, p.CustomerRelnCode, p.Status, p.CreatedBy)
	out, err := scanVfuParty(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrConflict)
	}
	return &out, err
}

func scanVfuParty(s interface{ Scan(...any) error }) (domain.VfuParty, error) {
	var p domain.VfuParty
	err := s.Scan(&p.ID, &p.TenantID, &p.PartyCode, &p.PartyName, &p.PartyType, &p.GenderCode,
		&p.DateOfBirth, &p.IdentificationID, &p.IssueDate, &p.IssuePlace, &p.MobileNumber,
		&p.PermanentAddress, &p.CustomerRelnCode, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (r *LoanRepository) ListVfuMandates(ctx context.Context, tenantID, q string) ([]domain.VfuMandate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, mandate_code, COALESCE(mandate_no,''), COALESCE(mandate_date::text,''), party_code,
		       COALESCE(org_code,''), COALESCE(rep_name,''), COALESCE(rep_phone,''), COALESCE(rep_address,''),
		       COALESCE(bank_name,''), COALESCE(bank_account,''), COALESCE(fee_payment_freq,''), rate_value,
		       status, created_by, created_at, updated_at
		FROM lnm_vfu_mandates
		WHERE tenant_id = $1 AND ($2 = '' OR mandate_code ILIKE '%' || $2 || '%' OR party_code ILIKE '%' || $2 || '%')
		ORDER BY mandate_code LIMIT 500`, tenantID, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.VfuMandate{}
	for rows.Next() {
		var m domain.VfuMandate
		if err := rows.Scan(&m.ID, &m.TenantID, &m.MandateCode, &m.MandateNo, &m.MandateDate, &m.PartyCode,
			&m.OrgCode, &m.RepName, &m.RepPhone, &m.RepAddress, &m.BankName, &m.BankAccount,
			&m.FeePaymentFreq, &m.RateValue, &m.Status, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateVfuMandate(ctx context.Context, m *domain.VfuMandate) (*domain.VfuMandate, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_vfu_mandates (id, tenant_id, mandate_code, mandate_no, mandate_date, party_code,
			org_code, rep_name, rep_phone, rep_address, bank_name, bank_account, fee_payment_freq,
			rate_value, status, created_by)
		VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (tenant_id, mandate_code) DO NOTHING
		RETURNING id, tenant_id, mandate_code, COALESCE(mandate_no,''), COALESCE(mandate_date::text,''), party_code,
		          COALESCE(org_code,''), COALESCE(rep_name,''), COALESCE(rep_phone,''), COALESCE(rep_address,''),
		          COALESCE(bank_name,''), COALESCE(bank_account,''), COALESCE(fee_payment_freq,''), rate_value,
		          status, created_by, created_at, updated_at`,
		m.ID, m.TenantID, m.MandateCode, m.MandateNo, m.MandateDate, m.PartyCode,
		m.OrgCode, m.RepName, m.RepPhone, m.RepAddress, m.BankName, m.BankAccount,
		m.FeePaymentFreq, m.RateValue, m.Status, m.CreatedBy)
	out := &domain.VfuMandate{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.MandateCode, &out.MandateNo, &out.MandateDate, &out.PartyCode,
		&out.OrgCode, &out.RepName, &out.RepPhone, &out.RepAddress, &out.BankName, &out.BankAccount,
		&out.FeePaymentFreq, &out.RateValue, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrConflict)
		}
		return nil, err
	}
	return out, nil
}

func (r *LoanRepository) ListVfuPlans(ctx context.Context, tenantID, mandateCode string) ([]domain.VfuPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, plan_code, COALESCE(plan_date::text,''), mandate_code, COALESCE(contract_code,''),
		       allocated_amt, settled_amt, fee_amt, status, created_by, created_at, updated_at
		FROM lnm_vfu_plans
		WHERE tenant_id = $1 AND ($2 = '' OR mandate_code = $2)
		ORDER BY plan_code LIMIT 500`, tenantID, mandateCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.VfuPlan{}
	for rows.Next() {
		var p domain.VfuPlan
		if err := rows.Scan(&p.ID, &p.TenantID, &p.PlanCode, &p.PlanDate, &p.MandateCode, &p.ContractCode,
			&p.AllocatedAmt, &p.SettledAmt, &p.FeeAmt, &p.Status, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateVfuPlan(ctx context.Context, p *domain.VfuPlan) (*domain.VfuPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_vfu_plans (id, tenant_id, plan_code, plan_date, mandate_code, contract_code,
			allocated_amt, settled_amt, fee_amt, status, created_by)
		VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, plan_code) DO NOTHING
		RETURNING id, tenant_id, plan_code, COALESCE(plan_date::text,''), mandate_code, COALESCE(contract_code,''),
		          allocated_amt, settled_amt, fee_amt, status, created_by, created_at, updated_at`,
		p.ID, p.TenantID, p.PlanCode, p.PlanDate, p.MandateCode, p.ContractCode,
		p.AllocatedAmt, p.SettledAmt, p.FeeAmt, p.Status, p.CreatedBy)
	out := &domain.VfuPlan{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.PlanCode, &out.PlanDate, &out.MandateCode, &out.ContractCode,
		&out.AllocatedAmt, &out.SettledAmt, &out.FeeAmt, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrConflict)
		}
		return nil, err
	}
	return out, nil
}

