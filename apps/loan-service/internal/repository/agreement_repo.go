package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

const contractColumns = `id, tenant_id, contract_code, contract_no, customer_code, employee_code,
	contract_type_code, product_code, interest_rate, interest_rate_type, purpose_code, industry_code,
	loan_method_code, contract_date::text, loan_term, term_unit, maturity_date::text,
	interest_schedule_day, loan_amt_minor, interest_payment_freq, principal_payment_freq,
	interest_payment_method, principal_payment_method, status, workflow_case_id::text,
	COALESCE(workflow_case_code,''), COALESCE(org_code,''), created_by, created_at, updated_at`

func scanContract(s interface{ Scan(...any) error }) (domain.Contract, error) {
	var c domain.Contract
	err := s.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.ContractNo, &c.CustomerCode, &c.EmployeeCode,
		&c.ContractTypeCode, &c.ProductCode, &c.InterestRate, &c.InterestRateType, &c.PurposeCode, &c.IndustryCode,
		&c.LoanMethodCode, &c.ContractDate, &c.LoanTerm, &c.TermUnit, &c.MaturityDate,
		&c.InterestScheduleDay, &c.LoanAmt, &c.InterestPaymentFreq, &c.PrincipalPaymentFreq,
		&c.InterestPaymentMethod, &c.PrincipalPaymentMethod, &c.Status, &c.WorkflowCaseID,
		&c.WorkflowCaseCode, &c.OrgCode, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
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

// contractSortCol maps the whitelisted ListContracts sort key to a SQL
// column; unknown keys fall back to created_at (the historical default).
func contractSortCol(field string) string {
	switch field {
	case "contract_no":
		return "contract_no"
	case "loan_amt_minor":
		return "loan_amt_minor"
	default:
		return "created_at"
	}
}

// ContractListFilter is the paged contract-list contract for GET
// /api/loan/contracts: status exact filter + q ILIKE (contract_no /
// customer_code / contract_code — the columns the legacy query matched) +
// whitelisted sort + SQL LIMIT/OFFSET.
type ContractListFilter struct {
	Status  string
	Search  string
	Sort    string
	Order   string
	Page    int
	PerPage int
}

// ListContractsPaged is the paged contract list: same narrowing as
// ListContracts plus the unfiltered total for the standard list envelope.
func (r *LoanRepository) ListContractsPaged(ctx context.Context, tenantID string, f ContractListFilter) ([]domain.Contract, int, error) {
	page := f.Page
	if page < 1 {
		page = 1
	}
	perPage := f.PerPage
	if perPage < 1 {
		perPage = 50
	}
	order := "DESC"
	if f.Sort != "" && f.Order != "desc" {
		order = "ASC"
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+contractColumns+`, count(*) OVER() AS total_count
		FROM lnm_contracts
		WHERE tenant_id = $1
		  AND ($2 = '' OR status = $2)
		  AND ($3 = '' OR contract_no ILIKE '%' || $3 || '%' OR customer_code ILIKE '%' || $3 || '%' OR contract_code ILIKE '%' || $3 || '%')
		ORDER BY `+contractSortCol(f.Sort)+` `+order+`, id
		LIMIT $4 OFFSET $5`, tenantID, f.Status, f.Search, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []domain.Contract{}
	total := 0
	for rows.Next() {
		var c domain.Contract
		var caseID sql.NullString
		// contractColumns scan (workflow_case_id nullable) + the window total.
		if err := rows.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.ContractNo, &c.CustomerCode, &c.EmployeeCode,
			&c.ContractTypeCode, &c.ProductCode, &c.InterestRate, &c.InterestRateType, &c.PurposeCode, &c.IndustryCode,
			&c.LoanMethodCode, &c.ContractDate, &c.LoanTerm, &c.TermUnit, &c.MaturityDate,
			&c.InterestScheduleDay, &c.LoanAmt, &c.InterestPaymentFreq, &c.PrincipalPaymentFreq,
			&c.InterestPaymentMethod, &c.PrincipalPaymentMethod, &c.Status, &caseID,
			&c.WorkflowCaseCode, &c.OrgCode, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &total); err != nil {
			return nil, 0, err
		}
		if caseID.Valid {
			c.WorkflowCaseID = &caseID.String
		}
		items = append(items, c)
	}
	return items, total, rows.Err()
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
			loan_amt_minor, interest_payment_freq, principal_payment_freq, interest_payment_method,
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

func (r *LoanRepository) UpdateContractStatus(ctx context.Context, tenantID, id, from, to string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE lnm_contracts SET status = $4, updated_at = now() WHERE tenant_id = $1 AND id = $2 AND status = $3`, tenantID, id, from, to)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var current string
		lookupErr := r.db.QueryRowContext(ctx, `SELECT status FROM lnm_contracts WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&current)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("%w", ErrNotFound)
		}
		if lookupErr != nil {
			return lookupErr
		}
		return fmt.Errorf("%w: contract is %s, expected %s", domain.ErrInvalidTransition, current, from)
	}
	return nil
}

// UpdateContract applies the maker-editable field whitelist (formation form:
// contract_no, amount/rate/term, dates, schedule and codes) to a contract.
// The status guard lives in the WHERE clause so a concurrent status flip
// (submit/decision) can never be overwritten: the UPDATE only matches
// DRAFT/PENDING_APPROVAL/REJECTED rows and a zero-row result distinguishes not-found vs
// not-editable via a cheap re-read.
func (r *LoanRepository) UpdateContract(ctx context.Context, tenantID, id string, in *domain.Contract) (*domain.Contract, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE lnm_contracts SET
			contract_no = $3, loan_amt_minor = $4, interest_rate = $5, loan_term = $6,
			term_unit = $7, contract_date = $8::date, maturity_date = $9::date,
			interest_schedule_day = $10, interest_payment_freq = $11,
			principal_payment_freq = $12, purpose_code = $13, employee_code = $14,
			industry_code = $15, loan_method_code = $16,
			status = CASE WHEN status = 'REJECTED' THEN 'DRAFT' ELSE status END,
			updated_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('DRAFT','PENDING_APPROVAL','REJECTED')
		RETURNING `+contractColumns,
		tenantID, id, in.ContractNo, in.LoanAmt, in.InterestRate, in.LoanTerm, in.TermUnit,
		in.ContractDate, in.MaturityDate, in.InterestScheduleDay, in.InterestPaymentFreq,
		in.PrincipalPaymentFreq, in.PurposeCode, in.EmployeeCode, in.IndustryCode, in.LoanMethodCode)
	out, err := scanContract(row)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		if scanErr := r.db.QueryRowContext(ctx,
			`SELECT 1 FROM lnm_contracts WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&exists); errors.Is(scanErr, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrNotFound)
		}
		return nil, fmt.Errorf("%w", ErrContractNotEditable)
	}
	return &out, err
}

func (r *LoanRepository) SetContractWorkflowCase(ctx context.Context, tenantID, id, caseID, caseCode string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE lnm_contracts SET workflow_case_id = $3, workflow_case_code = $4,
			status = 'PENDING_APPROVAL', updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		  AND (status = 'DRAFT' OR (status = 'PENDING_APPROVAL' AND workflow_case_id = $3))`, tenantID, id, caseID, caseCode)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrInvalidTransition
	}
	return nil
}

const agreementColumns = `id, tenant_id, contract_code, agreement_code, disburse_date::text, disburse_amt_minor,
	interest_rate, over_interest_rate, loan_term, term_unit, maturity_date::text, debt_group_code,
	interest_payment_freq, principal_payment_freq, outstanding_amt_minor, COALESCE(pending_disburse_amt_minor, 0), coln_principal_amt_minor, coln_interest_amt_minor,
	provision_amt_minor, COALESCE(plan_code, ''), currency_code, acc_classification, status, created_by, created_at, updated_at`

func scanAgreement(s interface{ Scan(...any) error }) (domain.Agreement, error) {
	var a domain.Agreement
	err := s.Scan(&a.ID, &a.TenantID, &a.ContractCode, &a.AgreementCode, &a.DisburseDate, &a.DisburseAmt,
		&a.InterestRate, &a.OverInterestRate, &a.LoanTerm, &a.TermUnit, &a.MaturityDate, &a.DebtGroupCode,
		&a.InterestPaymentFreq, &a.PrincipalPaymentFreq, &a.OutstandingAmt, &a.PendingDisburseAmt, &a.ColnPrincipalAmt, &a.ColnInterestAmt,
		&a.ProvisionAmt, &a.PlanCode, &a.CurrencyCode, &a.AccClassification, &a.Status, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func (r *LoanRepository) ListAgreements(ctx context.Context, tenantID, contractCode string) ([]domain.Agreement, error) {
	// Agreements list joins the contract header so the FE gets contract_no +
	// customer_code alongside the running balances (iteration 13 wave E).
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.tenant_id, a.contract_code, a.agreement_code, a.disburse_date::text, a.disburse_amt_minor,
		       a.interest_rate, a.over_interest_rate, a.loan_term, a.term_unit, a.maturity_date::text, a.debt_group_code,
		       a.interest_payment_freq, a.principal_payment_freq, a.outstanding_amt_minor, COALESCE(a.pending_disburse_amt_minor, 0),
		       a.coln_principal_amt_minor, a.coln_interest_amt_minor, a.provision_amt_minor, COALESCE(a.plan_code, ''),
		       a.currency_code, a.acc_classification, a.status, a.created_by, a.created_at, a.updated_at,
		       COALESCE(c.contract_no, ''), COALESCE(c.customer_code, '')
		FROM lnm_agreements a
		LEFT JOIN lnm_contracts c ON c.tenant_id = a.tenant_id AND c.contract_code = a.contract_code
		WHERE a.tenant_id = $1 AND ($2 = '' OR a.contract_code = $2)
		ORDER BY a.disburse_date DESC, a.agreement_code`, tenantID, contractCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Agreement{}
	for rows.Next() {
		var item domain.Agreement
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ContractCode, &item.AgreementCode, &item.DisburseDate, &item.DisburseAmt,
			&item.InterestRate, &item.OverInterestRate, &item.LoanTerm, &item.TermUnit, &item.MaturityDate, &item.DebtGroupCode,
			&item.InterestPaymentFreq, &item.PrincipalPaymentFreq, &item.OutstandingAmt, &item.PendingDisburseAmt,
			&item.ColnPrincipalAmt, &item.ColnInterestAmt, &item.ProvisionAmt, &item.PlanCode,
			&item.CurrencyCode, &item.AccClassification, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
			&item.ContractNo, &item.CustomerCode); err != nil {
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
	return getAgreementByCode(ctx, r.db, tenantID, agreementCode)
}

// getAgreementByCode reads one agreement through any query surface so
// callers can stay inside their own transaction.
func getAgreementByCode(ctx context.Context, q repoTX, tenantID, agreementCode string) (domain.Agreement, error) {
	row := q.QueryRowContext(ctx, `SELECT `+agreementColumns+` FROM lnm_agreements WHERE tenant_id = $1 AND agreement_code = $2`, tenantID, agreementCode)
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
	if a.CurrencyCode == "" {
		a.CurrencyCode = "VND"
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_agreements (id, tenant_id, contract_code, agreement_code, disburse_date, disburse_amt_minor,
				interest_rate, over_interest_rate, loan_term, term_unit, maturity_date, debt_group_code,
				interest_payment_freq, principal_payment_freq, outstanding_amt_minor, plan_code, currency_code, acc_classification, created_by)
			VALUES ($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11::date,$12,$13,$14,$15,$16,$17,$18,$19)
			ON CONFLICT (tenant_id, agreement_code) DO NOTHING
			RETURNING `+agreementColumns,
		a.ID, a.TenantID, a.ContractCode, a.AgreementCode, a.DisburseDate, a.DisburseAmt,
		a.InterestRate, a.OverInterestRate, a.LoanTerm, a.TermUnit, a.MaturityDate, a.DebtGroupCode,
		a.InterestPaymentFreq, a.PrincipalPaymentFreq, a.OutstandingAmt, a.PlanCode, a.CurrencyCode, a.AccClassification, a.CreatedBy)
	out, err := scanAgreement(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrConflict)
	}
	return &out, err
}

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
		       coll_address, quantity, unit_price_minor, coll_value_minor, coll_use_value_minor, deduction_ratio::float8, valuation_date::text, status,
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
		var deductionRatio float64
		if err := rows.Scan(&c.ID, &c.TenantID, &c.CollCode, &c.CollName, &c.CollTypeCode, &c.MortgageCode,
			&c.OwnerCifCode, &c.OwnerName, &c.CollAddress, &c.Quantity, &c.UnitPrice, &c.CollValue,
			&c.CollUseValue, &deductionRatio, &c.ValuationDate, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.DeductionRatio = &deductionRatio
		items = append(items, c)
	}
	return items, rows.Err()
}

func (r *LoanRepository) CreateCollateral(ctx context.Context, c *domain.Collateral) (*domain.Collateral, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO lnm_collaterals (id, tenant_id, coll_code, coll_name, coll_type_code, mortgage_code,
			owner_cif_code, owner_name, coll_address, quantity, unit_price_minor, coll_value_minor, coll_use_value_minor,
			deduction_ratio, valuation_date, status, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::date,$16,$17)
		ON CONFLICT (tenant_id, coll_code) DO NOTHING
		RETURNING id, tenant_id, coll_code, coll_name, coll_type_code, mortgage_code, owner_cif_code, owner_name,
		          coll_address, quantity, unit_price_minor, coll_value_minor, coll_use_value_minor, deduction_ratio::float8, valuation_date::text, status,
		          created_at, updated_at`,
		c.ID, c.TenantID, c.CollCode, c.CollName, c.CollTypeCode, c.MortgageCode, c.OwnerCifCode, c.OwnerName,
		c.CollAddress, c.Quantity, c.UnitPrice, c.CollValue, c.CollUseValue, c.DeductionRatio, c.ValuationDate, c.Status, c.CreatedAt)
	out, err := scanCollateral(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrConflict)
	}
	return &out, err
}

func scanCollateral(s interface{ Scan(...any) error }) (domain.Collateral, error) {
	var c domain.Collateral
	var deductionRatio float64
	err := s.Scan(&c.ID, &c.TenantID, &c.CollCode, &c.CollName, &c.CollTypeCode, &c.MortgageCode, &c.OwnerCifCode,
		&c.OwnerName, &c.CollAddress, &c.Quantity, &c.UnitPrice, &c.CollValue, &c.CollUseValue, &deductionRatio,
		&c.ValuationDate, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	c.DeductionRatio = &deductionRatio
	return c, err
}

func (r *LoanRepository) ListContractCollaterals(ctx context.Context, tenantID, contractCode string) ([]domain.ContractCollateral, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, contract_code, coll_code, coll_value_minor, created_at, updated_at
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
		INSERT INTO lnm_contract_collaterals (id, tenant_id, contract_code, coll_code, coll_value_minor)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (tenant_id, contract_code, coll_code) DO UPDATE SET coll_value_minor = EXCLUDED.coll_value_minor, updated_at = now()
		RETURNING id, tenant_id, contract_code, coll_code, coll_value_minor, created_at, updated_at`,
		cc.ID, cc.TenantID, cc.ContractCode, cc.CollCode, cc.CollValue)
	out := &domain.ContractCollateral{}
	err := row.Scan(&out.ID, &out.TenantID, &out.ContractCode, &out.CollCode, &out.CollValue, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

// GetContractByCode loads the contract header by business code.
func (r *LoanRepository) GetContractByCode(ctx context.Context, tenantID, code string) (domain.Contract, error) {
	var c domain.Contract
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, contract_code, COALESCE(contract_no,''), customer_code, employee_code,
		       contract_type_code, product_code, interest_rate, interest_rate_type, purpose_code,
		       industry_code, loan_method_code, contract_date::text, loan_term, term_unit,
		       COALESCE(maturity_date::text,''), interest_schedule_day, loan_amt_minor,
		       interest_payment_freq, principal_payment_freq, interest_payment_method,
		       principal_payment_method, status, COALESCE(org_code,''), created_by, created_at, updated_at
		FROM lnm_contracts WHERE tenant_id = $1 AND contract_code = $2`, tenantID, code)
	err := row.Scan(&c.ID, &c.TenantID, &c.ContractCode, &c.ContractNo, &c.CustomerCode, &c.EmployeeCode,
		&c.ContractTypeCode, &c.ProductCode, &c.InterestRate, &c.InterestRateType, &c.PurposeCode,
		&c.IndustryCode, &c.LoanMethodCode, &c.ContractDate, &c.LoanTerm, &c.TermUnit,
		&c.MaturityDate, &c.InterestScheduleDay, &c.LoanAmt,
		&c.InterestPaymentFreq, &c.PrincipalPaymentFreq, &c.InterestPaymentMethod,
		&c.PrincipalPaymentMethod, &c.Status, &c.OrgCode, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return c, fmt.Errorf("contract %s not found", code)
	}
	return c, err
}

// ApprovalLimit is one lnm_approval_limits row: the PGD / GD approval-tier
// thresholds (minor units) for one tenant+org (+ optional product).
type ApprovalLimit struct {
	OrgCode       string
	ProductCode   string
	PGDLimitMinor int64
	GDLimitMinor  int64
}

// GetApprovalLimits loads the lnm_approval_limits rows for a tenant+org: the
// exact product row and the org-wide fallback (product_code = ”) as two
// independent lookups — the product-vs-org precedence decision lives in the
// service layer (PickApprovalLimit) so it stays unit-testable without a DB.
// orgCode "" or a missing table row returns nil for that slot.
func (r *LoanRepository) GetApprovalLimits(ctx context.Context, tenantID, orgCode, productCode string) (product, org *ApprovalLimit, err error) {
	if orgCode == "" {
		return nil, nil, nil
	}
	load := func(code string) (*ApprovalLimit, error) {
		row := r.db.QueryRowContext(ctx, `
			SELECT org_code, product_code, pgd_limit_minor, gd_limit_minor
			FROM lnm_approval_limits
			WHERE tenant_id = $1 AND org_code = $2 AND product_code = $3`,
			tenantID, orgCode, code)
		var out ApprovalLimit
		if err := row.Scan(&out.OrgCode, &out.ProductCode, &out.PGDLimitMinor, &out.GDLimitMinor); err != nil {
			if err == sql.ErrNoRows {
				return nil, nil
			}
			return nil, err
		}
		return &out, nil
	}
	if product, err = load(productCode); err != nil {
		return nil, nil, err
	}
	if org, err = load(""); err != nil {
		return nil, nil, err
	}
	return product, org, nil
}

// Dossier aggregates one contract's full record set for the composite
// dossier page (fe_loan loan-management parity).
type Dossier struct {
	Contract      domain.Contract       `json:"contract"`
	Agreements    []domain.Agreement    `json:"agreements"`
	RepayPlans    []domain.RepayPlan    `json:"repay_plans"`
	Disbursements []domain.Disbursement `json:"disbursements"`
	Collections   []domain.Collection   `json:"collections"`
	Mortgages     []domain.Mortgage     `json:"mortgages"`
	Collaterals   []domain.Collateral   `json:"collaterals"`
	CaseIDs       []string              `json:"workflow_case_ids"`
}

// ListContractCaseIDs returns workflow case ids linked to a contract's
// transactions (adjustments + disbursements + collections).
func (r *LoanRepository) ListContractCaseIDs(ctx context.Context, tenantID, contractID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT workflow_case_id::text FROM lnm_disbursements WHERE tenant_id = $1 AND contract_code = (SELECT contract_code FROM lnm_contracts WHERE id = $2) AND workflow_case_id IS NOT NULL
		UNION ALL
		SELECT workflow_case_id::text FROM lnm_collections WHERE tenant_id = $1 AND contract_code = (SELECT contract_code FROM lnm_contracts WHERE id = $2) AND workflow_case_id IS NOT NULL
		LIMIT 200`, tenantID, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
