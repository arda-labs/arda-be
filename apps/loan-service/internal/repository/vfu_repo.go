package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
)

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
		       allocated_amt_minor, settled_amt_minor, fee_amt_minor, status, created_by, created_at, updated_at
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
			allocated_amt_minor, settled_amt_minor, fee_amt_minor, status, created_by)
		VALUES ($1,$2,$3,$4::date,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, plan_code) DO NOTHING
		RETURNING id, tenant_id, plan_code, COALESCE(plan_date::text,''), mandate_code, COALESCE(contract_code,''),
		          allocated_amt_minor, settled_amt_minor, fee_amt_minor, status, created_by, created_at, updated_at`,
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

// UpdateVfuParty edits a trust party. party_code and id are immutable.
func (r *LoanRepository) UpdateVfuParty(ctx context.Context, p *domain.VfuParty) (*domain.VfuParty, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE lnm_vfu_parties
		SET party_name = $3, party_type = $4, gender_code = $5, date_of_birth = NULLIF($6,'')::date,
		    identification_id = $7, issue_date = NULLIF($8,'')::date, issue_place = $9,
		    mobile_number = $10, permanent_address = $11, customer_reln_code = $12, status = $13,
		    updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, party_code, party_name, party_type, COALESCE(gender_code,''), COALESCE(date_of_birth::text,''),
		          COALESCE(identification_id,''), COALESCE(issue_date::text,''), COALESCE(issue_place,''),
		          COALESCE(mobile_number,''), COALESCE(permanent_address,''), COALESCE(customer_reln_code,''),
		          status, created_by, created_at, updated_at
	`, p.TenantID, p.ID, p.PartyName, p.PartyType, p.GenderCode, p.DateOfBirth, p.IdentificationID,
		p.IssueDate, p.IssuePlace, p.MobileNumber, p.PermanentAddress, p.CustomerRelnCode, p.Status)
	out, err := scanVfuParty(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w", ErrNotFound)
	}
	return &out, err
}

// UpdateVfuMandate edits a trust mandate. mandate_code and id are immutable.
func (r *LoanRepository) UpdateVfuMandate(ctx context.Context, m *domain.VfuMandate) (*domain.VfuMandate, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE lnm_vfu_mandates
		SET mandate_no = $3, mandate_date = NULLIF($4,'')::date, party_code = $5, org_code = $6,
		    rep_name = $7, rep_phone = $8, rep_address = $9, bank_name = $10, bank_account = $11,
		    fee_payment_freq = $12, rate_value = $13, status = $14, updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, mandate_code, COALESCE(mandate_no,''), COALESCE(mandate_date::text,''), party_code,
		          COALESCE(org_code,''), COALESCE(rep_name,''), COALESCE(rep_phone,''), COALESCE(rep_address,''),
		          COALESCE(bank_name,''), COALESCE(bank_account,''), COALESCE(fee_payment_freq,''), rate_value,
		          status, created_by, created_at, updated_at
	`, m.TenantID, m.ID, m.MandateNo, m.MandateDate, m.PartyCode, m.OrgCode, m.RepName, m.RepPhone,
		m.RepAddress, m.BankName, m.BankAccount, m.FeePaymentFreq, m.RateValue, m.Status)
	out := &domain.VfuMandate{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.MandateCode, &out.MandateNo, &out.MandateDate, &out.PartyCode,
		&out.OrgCode, &out.RepName, &out.RepPhone, &out.RepAddress, &out.BankName, &out.BankAccount,
		&out.FeePaymentFreq, &out.RateValue, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrNotFound)
		}
		return nil, err
	}
	return out, nil
}

// UpdateVfuPlan edits a funding plan. plan_code and id are immutable.
func (r *LoanRepository) UpdateVfuPlan(ctx context.Context, p *domain.VfuPlan) (*domain.VfuPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE lnm_vfu_plans
		SET plan_date = NULLIF($3,'')::date, mandate_code = $4, contract_code = $5,
		    allocated_amt_minor = $6, settled_amt_minor = $7, fee_amt_minor = $8, status = $9,
		    updated_at = now()
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, tenant_id, plan_code, COALESCE(plan_date::text,''), mandate_code, COALESCE(contract_code,''),
		          allocated_amt_minor, settled_amt_minor, fee_amt_minor, status, created_by, created_at, updated_at
	`, p.TenantID, p.ID, p.PlanDate, p.MandateCode, p.ContractCode, p.AllocatedAmt, p.SettledAmt, p.FeeAmt, p.Status)
	out := &domain.VfuPlan{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.PlanCode, &out.PlanDate, &out.MandateCode, &out.ContractCode,
		&out.AllocatedAmt, &out.SettledAmt, &out.FeeAmt, &out.Status, &out.CreatedBy, &out.CreatedAt, &out.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w", ErrNotFound)
		}
		return nil, err
	}
	return out, nil
}
