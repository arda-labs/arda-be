package repository

import (
	"context"
	"fmt"
	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	"github.com/shopspring/decimal"
	"time"
)

func (r *LoanRepository) ListRepayPlans(ctx context.Context, tenantID, contractCode, agreementCode string) ([]domain.RepayPlan, error) {
	// Only the active version is exposed: a restructure retires the previous
	// schedule (is_active = FALSE) instead of deleting it, so collected
	// history survives in the table while the working schedule stays clean.
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, contract_code, agreement_code, plan_no, term_no, from_date::text, to_date::text,
		       interest_rate, plan_principal_amt_minor, plan_interest_amt_minor, coln_principal_amt_minor, coln_interest_amt_minor,
		       is_active, lifecycle_status, payment_status, created_at, updated_at
		FROM lnm_repay_plans
		WHERE tenant_id = $1 AND is_active
		  AND ($2 = '' OR contract_code = $2) AND ($3 = '' OR agreement_code = $3)
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
			&p.ColnPrincipalAmt, &p.ColnInterestAmt, &p.IsActive, &p.LifecycleStatus, &p.PaymentStatus,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// ReplaceRepayPlans publishes a new active schedule version for one
// agreement (restructure / plan generation semantics — mirrors EPAS plan
// regeneration). The previous version is retired with is_active = FALSE, it
// is never deleted: its coln_* snapshot is the record of what was already
// collected, and dropping it would make the system believe the customer
// never paid. The new rows are the only ones readers select (is_active), and
// their totals are computed from the agreement's current outstanding balance,
// so the collected amounts are not double-counted either.
func (r *LoanRepository) ReplaceRepayPlans(ctx context.Context, tenantID, agreementCode string, plans []domain.RepayPlan) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := replaceRepayPlansTx(ctx, tx, tenantID, agreementCode, plans); err != nil {
		return err
	}
	return tx.Commit()
}

// replaceRepayPlansTx retires the current active version and inserts the new
// one through the caller's query surface, so a restructure decision can do
// the agreement update and the schedule version swap atomically.
func replaceRepayPlansTx(ctx context.Context, q repoTX, tenantID, agreementCode string, plans []domain.RepayPlan) error {
	if err := domain.CanTransition(domain.PlanLifecycleMachine, domain.StatusActive, domain.StatusSuperseded, ""); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `
		UPDATE lnm_repay_plans SET is_active = FALSE, lifecycle_status = 'SUPERSEDED', updated_at = now()
		WHERE tenant_id = $1 AND agreement_code = $2 AND is_active`, tenantID, agreementCode); err != nil {
		return err
	}
	for i := range plans {
		p := &plans[i]
		if p.ID == "" {
			p.ID = NewID("plan")
		}
		if p.AgreementCode == "" {
			p.AgreementCode = agreementCode
		}
		if _, err := q.ExecContext(ctx, `
			INSERT INTO lnm_repay_plans (id, tenant_id, contract_code, agreement_code, plan_no, term_no,
				from_date, to_date, interest_rate, plan_principal_amt_minor, plan_interest_amt_minor,
				coln_principal_amt_minor, coln_interest_amt_minor, is_active, lifecycle_status)
			VALUES ($1,$2,$3,$4,$5,$6,$7::date,$8::date,$9,$10,$11,$12,$13,TRUE,'ACTIVE')`,
			p.ID, tenantID, p.ContractCode, p.AgreementCode, p.PlanNo, p.TermNo,
			p.FromDate, p.ToDate, p.InterestRate, p.PlanPrincipalAmt, p.PlanInterestAmt,
			p.ColnPrincipalAmt, p.ColnInterestAmt); err != nil {
			return err
		}
	}
	return nil
}

// RegeneratePlans publishes a new active schedule version for one agreement
// (restructure semantics): the outstanding balance is spread over termCount
// months at the agreement's current rate. The previous version is retired
// with is_active = FALSE, never deleted — see ReplaceRepayPlans.
func (r *LoanRepository) RegeneratePlans(ctx context.Context, tenantID, agreementCode string, termCount int, startDate string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := regeneratePlansTx(ctx, tx, tenantID, agreementCode, termCount, startDate); err != nil {
		return err
	}
	return tx.Commit()
}

// regeneratePlansTx builds and publishes the new schedule through the
// caller's query surface (a restructure resolve runs it inside the decision
// transaction).
func regeneratePlansTx(ctx context.Context, q repoTX, tenantID, agreementCode string, termCount int, startDate string) error {
	agreement, err := getAgreementByCode(ctx, q, tenantID, agreementCode)
	if err != nil {
		return err
	}
	plans, err := buildEvenPrincipalPlans(agreement, termCount, startDate)
	if err != nil {
		return err
	}
	return replaceRepayPlansTx(ctx, q, tenantID, agreementCode, plans)
}

// buildEvenPrincipalPlans is the pure schedule math for a restructure: an
// even-principal monthly schedule whose principal shares sum exactly to the
// agreement's outstanding balance (the LoanReconciliation invariant) and
// whose interest is charged on the declining balance at the agreement rate.
// Money math goes through arda-money (decimal + currency rounding), never
// float64.
func buildEvenPrincipalPlans(agreement domain.Agreement, termCount int, startDate string) ([]domain.RepayPlan, error) {
	if termCount <= 0 {
		return nil, fmt.Errorf("term count must be positive, got %d", termCount)
	}
	currency := agreement.CurrencyCode
	if currency == "" {
		currency = "VND"
	}
	outstanding := ardamoney.FromMinor(agreement.OutstandingAmt, currency)
	rate := decimal.NewFromFloat(agreement.InterestRate)
	shares := ardamoney.AllocateEven(outstanding, termCount, currency)

	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return nil, fmt.Errorf("invalid effective start date %q: %w", startDate, err)
	}
	plans := make([]domain.RepayPlan, 0, termCount)
	remaining := outstanding
	for i := 1; i <= termCount; i++ {
		principal := shares[i-1]
		from := start.AddDate(0, i-1, 0)
		to := start.AddDate(0, i, 0)
		plans = append(plans, domain.RepayPlan{
			ContractCode:     agreement.ContractCode,
			AgreementCode:    agreement.AgreementCode,
			PlanNo:           1,
			TermNo:           i,
			FromDate:         from.Format("2006-01-02"),
			ToDate:           to.Format("2006-01-02"),
			InterestRate:     agreement.InterestRate,
			PlanPrincipalAmt: ardamoney.MustToMinor(principal, currency),
			PlanInterestAmt:  ardamoney.MustToMinor(ardamoney.MonthlyInterest(remaining, rate, currency), currency),
		})
		remaining = remaining.Sub(principal)
	}
	return plans, nil
}

// ReducePlanInterest applies an interest waiver across unpaid active schedule
// rows (coln < plan), earliest due date first, until the waiver amount is
// consumed. It owns its transaction; callers already inside one (adjustment
// resolve) use reducePlanInterestTx.
func (r *LoanRepository) ReducePlanInterest(ctx context.Context, tenantID, agreementCode string, waiverAmountMinor int64) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	applied, err := reducePlanInterestTx(ctx, tx, tenantID, agreementCode, waiverAmountMinor)
	if err != nil {
		return applied, err
	}
	if err := tx.Commit(); err != nil {
		return applied, err
	}
	return applied, nil
}

// reducePlanInterestTx shaves unpaid schedule interest through the caller's
// query surface so a waiver resolve stays in one transaction.
func reducePlanInterestTx(ctx context.Context, q repoTX, tenantID, agreementCode string, waiverAmountMinor int64) (int64, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, plan_interest_amt_minor - coln_interest_amt_minor
		FROM lnm_repay_plans
		WHERE tenant_id = $1 AND agreement_code = $2 AND is_active
		  AND plan_interest_amt_minor > coln_interest_amt_minor
		ORDER BY to_date`, tenantID, agreementCode)
	if err != nil {
		return 0, err
	}
	type target struct {
		id  string
		due int64
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

	remaining := waiverAmountMinor
	applied := int64(0)
	for _, t := range targets {
		if remaining <= 0 {
			break
		}
		reduce := t.due
		if reduce > remaining {
			reduce = remaining
		}
		if _, err := q.ExecContext(ctx, `
			UPDATE lnm_repay_plans SET plan_interest_amt_minor = plan_interest_amt_minor - $3, updated_at = now()
			WHERE tenant_id = $1 AND id = $2`, tenantID, t.id, reduce); err != nil {
			return applied, err
		}
		remaining -= reduce
		applied += reduce
	}
	return applied, nil
}
