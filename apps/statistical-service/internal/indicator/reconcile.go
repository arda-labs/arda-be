package indicator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
)

// ReconcileResult is the outcome of checking one indicator against an
// independent recomputation.
type ReconcileResult struct {
	Code   string
	Name   string
	Engine float64
	Direct float64
	Match  bool
	Detail string
}

// ReconciliationReport summarizes a reconciliation run.
type ReconciliationReport struct {
	PeriodCode    string            `json:"period_code"`
	Checked       int               `json:"checked"`
	Mismatches    []ReconcileResult `json:"mismatches,omitempty"`
	TrialBalanced bool              `json:"trial_balanced"`
	DebitTotal    int64             `json:"debit_total_minor"`
	CreditTotal   int64             `json:"credit_total_minor"`
}

// ReconcileIndicatorFormulas is the entry point the service uses: it takes the
// stored formulas of the account-type indicators and reconciles the ones that
// are account-balance shaped. Codes without an account_balance formula are
// ignored (they are not backed by the trial balance).
func ReconcileIndicatorFormulas(ctx context.Context, repo *repository.StatisticalRepository, tenantID, periodCode string, formulas map[string]json.RawMessage) (ReconciliationReport, error) {
	blocks := map[string]*accountBalance{}
	for code, raw := range formulas {
		var f Formula
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		if f.Type != "account_balance" || f.Accounts == nil {
			continue
		}
		blocks[code] = f.Accounts
	}
	if len(blocks) == 0 {
		return ReconciliationReport{PeriodCode: periodCode, TrialBalanced: true}, nil
	}
	return ReconcileAccounting(ctx, repo, tenantID, periodCode, blocks)
}

// ReconcileAccounting cross-checks the account_balance indicators against the
// trial-balance fact.
//
// Two independent checks run:
//
//  1. The trial balance must balance: SUM(close_debit) == SUM(close_credit)
//     across every account of the tenant/date. This validates the fact and ETL.
//  2. Each parsed formula is recomputed with a hand-written SUM over the same
//     account prefixes and compared to the engine's value. Because the direct
//     recomputation shares the fact but not the engine's SQL builder, a bug in
//     the builder (mis-signed term, dropped prefix, wrong clamp) shows up as a
//     mismatch instead of a silent wrong number.
//
// The caller supplies the parsed blocks; the function does not seed anything.
func ReconcileAccounting(ctx context.Context, repo *repository.StatisticalRepository, tenantID, periodCode string, blocks map[string]*accountBalance) (ReconciliationReport, error) {
	report := ReconciliationReport{PeriodCode: periodCode}

	debits, credits, err := repo.TrialBalanceTotals(ctx, tenantID, periodCode)
	if err != nil {
		return report, fmt.Errorf("trial balance totals: %w", err)
	}
	report.DebitTotal = debits
	report.CreditTotal = credits
	report.TrialBalanced = debits == credits

	engine := &Engine{repo: repo}
	for code, block := range blocks {
		if block == nil {
			continue
		}
		got, err := engine.evalAccountBalance(ctx, tenantID, periodCode, block, nil)
		if err != nil {
			return report, fmt.Errorf("reconcile %s: %w", code, err)
		}
		want, err := directAccountSum(ctx, repo, tenantID, periodCode, block)
		if err != nil {
			return report, fmt.Errorf("reconcile %s direct: %w", code, err)
		}
		report.Checked++
		if !almostEqual(got, want) {
			report.Mismatches = append(report.Mismatches, ReconcileResult{
				Code:   code,
				Engine: got,
				Direct: want,
				Match:  false,
				Detail: "engine and direct recomputation disagree",
			})
		}
	}
	return report, nil
}

// directAccountSum recomputes an account_balance block with a straight,
// engine-independent SQL statement: one SUM per term with its sign folded into
// the arithmetic, no FILTER/GREATEST from the engine's builder.
func directAccountSum(ctx context.Context, repo *repository.StatisticalRepository, tenantID, periodCode string, block *accountBalance) (float64, error) {
	if err := block.validate(); err != nil {
		return 0, err
	}
	// Guard: this helper intentionally reimplements the arithmetic, so it only
	// supports the shapes it models. A clamp or net sign it cannot mirror would
	// make the comparison meaningless.
	for _, term := range block.Terms {
		if term.Clamp != "" {
			return 0, fmt.Errorf("direct check does not support clamped terms")
		}
	}

	args := []any{tenantID, periodCode + "-01"}
	asOf := "business_date = (SELECT max(business_date) FROM " + block.Fact +
		" WHERE tenant_id = $1 AND business_date <= ($2::date + INTERVAL '1 month' - INTERVAL '1 day')::date)"

	parts := []string{"0"}
	for _, term := range block.Terms {
		like := make([]string, 0, len(term.Prefixes))
		for _, p := range term.Prefixes {
			args = append(args, p+"%")
			like = append(like, "account_code LIKE $"+itoa(len(args)))
		}
		predicate := "(" + strings.Join(like, " OR ") + ")"
		for _, x := range term.Exclude {
			args = append(args, x+"%")
			predicate += " AND account_code NOT LIKE $" + itoa(len(args))
		}
		side := term.Side
		col := "close_debit_minor"
		if side == "credit" {
			col = "close_credit_minor"
		}
		negation := ""
		if term.Sign != nil && *term.Sign == -1 {
			negation = "-"
		}
		if side == "net" {
			parts = append(parts, fmt.Sprintf("%s(COALESCE(SUM(close_debit_minor) FILTER (WHERE %s),0) - COALESCE(SUM(close_credit_minor) FILTER (WHERE %s),0))",
				negation, predicate, predicate))
		} else {
			parts = append(parts, fmt.Sprintf("%s(COALESCE(SUM(%s) FILTER (WHERE %s),0))", negation, col, predicate))
		}
	}
	sqlText := "SELECT (" + strings.Join(parts, " + ") + ")::numeric FROM " + block.Fact + " WHERE tenant_id = $1 AND " + asOf
	return repo.ScalarQuery(ctx, sqlText, args)
}

func almostEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	// Amounts are minor units; a difference under half a unit is rounding.
	return d < 0.5
}
