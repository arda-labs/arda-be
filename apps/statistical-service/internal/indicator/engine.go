// Package indicator computes indicator values from the declarative `formula`
// JSON stored on rpt_indicators (arda-be/docs/reporting-data-layer.md §5.3).
//
// The engine is the deliberate replacement for EPAS's SQL-as-config
// (CTG_CFG_STAT_KPI.EXPRESSION_SQL, a 4000-char SQL string per KPI): a formula
// names a fact table, a column, an aggregate, an optional filter and an as-of
// anchor, and this package — not the database — turns that into SQL against a
// closed whitelist. Stored text is never executed, and a formula naming an
// unknown table/column/aggregate fails closed.
package indicator

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
)

// Formula is the declarative shape stored in rpt_indicators.formula.
//
//	sample:  {"type":"sum","fact":"rpt_fact_loan_agreement_daily",
//	          "column":"outstanding_amt_minor","filter":{"status":"ACTIVE"},
//	          "as_of":"period_end","scale":100}
//	ratio:   {"type":"ratio","numerator":{...},"denominator":{...},"percent":true}
//	growth:  {"type":"growth","indicator":"60000.01","compare":"previous_period","percent":true}
//	avg:     {"type":"trailing_average","indicator":"20000.01","months":3}
type Formula struct {
	Type string `json:"type"`

	// Leaf members (sum/count/count_distinct/average).
	Fact   string                     `json:"fact,omitempty"`
	Column string                     `json:"column,omitempty"`
	Filter map[string]json.RawMessage `json:"filter,omitempty"`
	AsOf   string                     `json:"as_of,omitempty"`
	Scale  float64                    `json:"scale,omitempty"`

	// Ratio members.
	Numerator   *Formula `json:"numerator,omitempty"`
	Denominator *Formula `json:"denominator,omitempty"`
	Percent     bool     `json:"percent,omitempty"`

	// Growth member (references another indicator code).
	Indicator string `json:"indicator,omitempty"`
	Compare   string `json:"compare,omitempty"`

	// Account balance (trial-balance indicators): a linear combination of
	// account-code prefixes.
	Accounts *accountBalance `json:"accounts,omitempty"`
}

// fact columns: the closed whitelist the engine may aggregate. A new fact table
// must be registered here before any indicator can reference it.
var factColumns = map[string]map[string]bool{
	"rpt_fact_loan_agreement_daily": {
		"outstanding_amt_minor": true, "provision_amt_minor": true,
		"disburse_amt_minor": true, "interest_rate": true,
		"debt_group_code": true, "status": true, "org_code": true,
		"customer_code": true, "product_code": true,
		"loan_term_months": true, "term_bucket": true, "loan_method_code": true,
		"industry_code": true, "purpose_code": true,
	},
	"rpt_fact_deposit_contract_daily": {
		"principal_minor": true, "accrued_minor": true, "status": true,
		"org_code": true, "customer_code": true, "product_code": true,
	},
	"rpt_fact_loan_collateral_daily": {
		"coll_value_minor": true, "coll_use_value_minor": true,
		"coll_type_code": true, "status": true, "org_code": true,
	},
	"rpt_fact_capital_contract_daily": {
		"amount_minor": true, "interest_rate": true, "status": true,
		"org_code": true, "fund_type_code": true, "counterparty_code": true,
	},
	"rpt_fact_capital_movement_daily": {
		"amount_minor": true, "movement_type": true, "status": true,
	},
	"rpt_fact_customer_daily": {
		"customer_code": true, "status": true, "org_code": true,
		"segment": true, "customer_type": true, "risk_level": true,
	},
	"rpt_fact_member_daily": {
		"member_code": true, "customer_code": true, "org_code": true,
		"member_type_code": true, "member_status": true,
		"estb_capital_minor": true, "add_capital_minor": true, "total_capital_minor": true,
	},
	"rpt_fact_member_request_daily": {
		"member_code": true, "org_code": true, "request_type": true,
		"status": true, "amount_minor": true,
	},
	"rpt_fact_ibm_deposit_daily": {
		"deposit_code": true, "counterparty_code": true, "product_code": true,
		"term_months": true, "status": true, "currency_code": true,
		"org_code": true, "principal_minor": true, "accrued_minor": true,
		"interest_rate": true,
	},
	"rpt_fact_trial_balance_daily": {
		"org_code": true, "coa_version": true, "account_code": true,
		"account_name": true, "currency_code": true,
		"open_debit_minor": true, "open_credit_minor": true,
		"incr_debit_minor": true, "incr_credit_minor": true,
		"close_debit_minor": true, "close_credit_minor": true,
	},
	"rpt_fact_ibm_borrow_daily": {
		"borrow_code": true, "counterparty_code": true, "lender_type": true,
		"funding_purpose": true, "term_months": true, "maturity_status": true,
		"status": true, "currency_code": true, "org_code": true,
		"principal_minor": true, "outstanding_minor": true, "accrued_minor": true,
		"interest_rate": true,
	},
}

// numericFactColumns marks which whitelisted fact columns hold numbers, so a
// comparison filter compares them as numbers. Postgres has no bigint > text
// operator: without this, `total_capital_minor > "0"` fails SQLSTATE 42883. Text
// comparison would also be wrong for numbers ("9" > "10").
var numericFactColumns = map[string]map[string]bool{
	"rpt_fact_loan_agreement_daily": {
		"outstanding_amt_minor": true, "provision_amt_minor": true,
		"disburse_amt_minor": true, "interest_rate": true,
		"loan_term_months": true,
	},
	"rpt_fact_deposit_contract_daily": {
		"principal_minor": true, "accrued_minor": true,
	},
	"rpt_fact_loan_collateral_daily": {
		"coll_value_minor": true, "coll_use_value_minor": true,
	},
	"rpt_fact_capital_contract_daily": {
		"amount_minor": true, "interest_rate": true,
	},
	"rpt_fact_capital_movement_daily": {
		"amount_minor": true,
	},
	"rpt_fact_member_daily": {
		"estb_capital_minor": true, "add_capital_minor": true, "total_capital_minor": true,
	},
	"rpt_fact_member_request_daily": {
		"amount_minor": true,
	},
	"rpt_fact_ibm_deposit_daily": {
		"term_months": true, "principal_minor": true, "accrued_minor": true,
		"interest_rate": true,
	},
	"rpt_fact_trial_balance_daily": {
		"open_debit_minor": true, "open_credit_minor": true,
		"incr_debit_minor": true, "incr_credit_minor": true,
		"close_debit_minor": true, "close_credit_minor": true,
	},
	"rpt_fact_ibm_borrow_daily": {
		"term_months": true, "principal_minor": true, "outstanding_minor": true,
		"accrued_minor": true, "interest_rate": true,
	},
}

// dimDef maps an indicator dimension name to the fact column it slices on.
// Only these dimensions are accepted in a compute request.
var dimDef = map[string]string{
	"org":          "org_code",
	"product":      "product_code",
	"fund_type":    "fund_type_code",
	"coll_type":    "coll_type_code",
	"segment":      "segment",
	"debt_group":   "debt_group_code",
	"member_type":  "member_type_code",
	"counterparty": "counterparty_code",
	"term":         "term_months",
	"loan_term":    "term_bucket",
	"loan_method":  "loan_method_code",
	"industry":     "industry_code",
	"purpose":      "purpose_code",
	"lender_type":  "lender_type",
	"maturity":     "maturity_status",
}

// DimensionNames is the sorted list of accepted dimension names (stable order
// for API parameter parsing).
var DimensionNames = func() []string {
	names := make([]string, 0, len(dimDef))
	for name := range dimDef {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}()

// DimensionKey renders a dims map into the deterministic storage key used by
// rpt_indicator_results.dimension_key (e.g. "org=01|product=DPM12"). An empty
// dims map yields "" (the total row).
func DimensionKey(dims map[string]string) string {
	if len(dims) == 0 {
		return ""
	}
	names := make([]string, 0, len(dims))
	for name := range dims {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+dims[name])
	}
	return strings.Join(parts, "|")
}

// Engine computes indicator values through the repository's scalar query.
type Engine struct {
	repo *repository.StatisticalRepository
}

func NewEngine(repo *repository.StatisticalRepository) *Engine {
	return &Engine{repo: repo}
}

// Compute evaluates one indicator for one period.
//
//   - dims maps a dimension name (org, product, …) to its value; the engine
//     translates it into the fact filter. Empty dims ⇒ a total.
//   - periodCode is "YYYY-MM"; the as-of date is the period end.
//
// It returns the raw (unrounded) value; formatting is the caller's job.
func (e *Engine) Compute(ctx context.Context, tenantID, code, periodCode string, dims map[string]string) (float64, error) {
	ind, err := e.repo.GetIndicatorByCode(ctx, tenantID, code)
	if err != nil {
		return 0, fmt.Errorf("load indicator %s: %w", code, err)
	}
	if ind == nil {
		return 0, fmt.Errorf("indicator not found: %s", code)
	}
	if len(ind.Formula) == 0 {
		return 0, fmt.Errorf("indicator %s has no formula", code)
	}
	var f Formula
	if err := json.Unmarshal(ind.Formula, &f); err != nil {
		return 0, fmt.Errorf("indicator %s: invalid formula: %w", code, err)
	}
	if len(dims) > 0 && ind.KpiType == "P" {
		// Dimensions are applied to the fact filter below.
	}
	return e.eval(ctx, tenantID, periodCode, &f, dims, 0)
}

// maxDepth bounds ratio/growth nesting so a self-referential formula fails
// closed instead of recursing forever.
const maxDepth = 4

// defaultTrailingPeriods is how many months ComputeSeries loads by default:
// enough for 3-month averages and previous-period growth.
const defaultTrailingPeriods = 6

// PeriodResolver loads one indicator's stored value for a period (nil when
// absent). Kept as a function so the engine's series math has no direct
// repository dependency.
type PeriodResolver func(ctx context.Context, indicatorCode, periodCode string) (*float64, error)

// ComputeSeries evaluates an indicator that needs period-over-period context:
// growth (vs previous period / same period last year) and trailing averages.
// It loads up to `trailing` months ending at periodCode and then applies the
// series math. Non-series formulas fall back to the single-period path.
func (e *Engine) ComputeSeries(ctx context.Context, tenantID, code, periodCode string, dims map[string]string, resolve PeriodResolver, trailing int) (float64, error) {
	ind, err := e.repo.GetIndicatorByCode(ctx, tenantID, code)
	if err != nil {
		return 0, fmt.Errorf("load indicator %s: %w", code, err)
	}
	if ind == nil {
		return 0, fmt.Errorf("indicator not found: %s", code)
	}
	if len(ind.Formula) == 0 {
		return 0, fmt.Errorf("indicator %s has no formula", code)
	}
	var f Formula
	if err := json.Unmarshal(ind.Formula, &f); err != nil {
		return 0, fmt.Errorf("indicator %s: invalid formula: %w", code, err)
	}
	if f.Type != "growth" && f.Type != "trailing_average" {
		return e.eval(ctx, tenantID, periodCode, &f, dims, 0)
	}
	if resolve == nil {
		return 0, fmt.Errorf("series formula requires a period resolver")
	}
	if trailing <= 0 {
		trailing = defaultTrailingPeriods
	}
	periods, err := trailingPeriods(periodCode, trailing)
	if err != nil {
		return 0, err
	}
	series := make(map[string]float64, len(periods))
	for _, p := range periods {
		v, err := resolve(ctx, f.Indicator, p)
		if err != nil {
			return 0, fmt.Errorf("load %s for %s: %w", f.Indicator, p, err)
		}
		if v != nil {
			series[p] = *v
		}
	}
	return e.evalSeries(&f, periodCode, series)
}

// evalSeries applies growth/trailing_average math to loaded period values.
func (e *Engine) evalSeries(f *Formula, periodCode string, series map[string]float64) (float64, error) {
	switch f.Type {
	case "growth":
		if f.Indicator == "" {
			return 0, fmt.Errorf("growth requires an indicator code")
		}
		current, ok := series[periodCode]
		if !ok {
			return 0, fmt.Errorf("no stored value for %s in %s (compute the base indicator first)", f.Indicator, periodCode)
		}
		var comparePeriod string
		switch f.Compare {
		case "", "previous_period":
			prev, ok := previousPeriod(periodCode)
			if !ok {
				return 0, fmt.Errorf("cannot resolve previous period for %s", periodCode)
			}
			comparePeriod = prev
		case "same_period_last_year":
			py, err := periodYearsAgo(periodCode, 1)
			if err != nil {
				return 0, err
			}
			comparePeriod = py
		default:
			return 0, fmt.Errorf("compare must be previous_period or same_period_last_year (got %q)", f.Compare)
		}
		base, ok := series[comparePeriod]
		if !ok || base == 0 {
			// No base ⇒ growth is undefined; report 0 rather than a fake spike.
			return 0, nil
		}
		g := (current - base) / base
		if f.Percent {
			g *= 100
		}
		return g, nil

	case "trailing_average":
		var sum float64
		var n int
		for _, p := range trailingPeriodsFrom(periodCode, series) {
			if v, ok := series[p]; ok {
				sum += v
				n++
			}
		}
		if n == 0 {
			return 0, nil
		}
		return sum / float64(n), nil
	default:
		return 0, fmt.Errorf("unknown series formula type %q", f.Type)
	}
}

// trailingPeriods returns `count` canonical YYYY-MM periods ending at end
// (inclusive), oldest first.
func trailingPeriods(end string, count int) ([]string, error) {
	t, err := time.Parse("2006-01", end)
	if err != nil {
		return nil, fmt.Errorf("period_code must be YYYY-MM: %w", err)
	}
	out := make([]string, 0, count)
	for i := count - 1; i >= 0; i-- {
		out = append(out, t.AddDate(0, -i, 0).Format("2006-01"))
	}
	return out, nil
}

// trailingPeriodsFrom lists the periods present in a loaded series, oldest
// first (used by trailing_average so it averages exactly what was loaded).
func trailingPeriodsFrom(end string, series map[string]float64) []string {
	periods := make([]string, 0, len(series))
	for p := range series {
		periods = append(periods, p)
	}
	sort.Strings(periods)
	return periods
}

// previousPeriod returns the calendar month before p.
func previousPeriod(p string) (string, bool) {
	t, err := time.Parse("2006-01", p)
	if err != nil {
		return "", false
	}
	return t.AddDate(0, -1, 0).Format("2006-01"), true
}

// periodYearsAgo returns the same calendar month n years earlier.
func periodYearsAgo(p string, n int) (string, error) {
	t, err := time.Parse("2006-01", p)
	if err != nil {
		return "", fmt.Errorf("period_code must be YYYY-MM: %w", err)
	}
	return t.AddDate(-n, 0, 0).Format("2006-01"), nil
}

func (e *Engine) eval(ctx context.Context, tenantID, periodCode string, f *Formula, dims map[string]string, depth int) (float64, error) {
	if depth > maxDepth {
		return 0, fmt.Errorf("formula nesting exceeds %d levels", maxDepth)
	}
	switch f.Type {
	case "sum", "count", "count_distinct", "average":
		return e.leaf(ctx, tenantID, periodCode, f, dims)
	case "account_balance":
		if f.Accounts == nil {
			return 0, fmt.Errorf("account_balance requires the accounts block")
		}
		return e.evalAccountBalance(ctx, tenantID, periodCode, f.Accounts, dims)
	case "ratio":
		if f.Numerator == nil || f.Denominator == nil {
			return 0, fmt.Errorf("ratio requires numerator and denominator")
		}
		num, err := e.eval(ctx, tenantID, periodCode, f.Numerator, dims, depth+1)
		if err != nil {
			return 0, err
		}
		den, err := e.eval(ctx, tenantID, periodCode, f.Denominator, dims, depth+1)
		if err != nil {
			return 0, err
		}
		return ratioValue(num, den, f.Percent), nil
	case "growth":
		if f.Indicator == "" {
			return 0, fmt.Errorf("growth requires an indicator code")
		}
		// Growth is computed by the caller across periods: the engine cannot
		// reach a previous period without the caller's period list, so a
		// growth formula resolves through ComputeSeries instead.
		return 0, fmt.Errorf("growth must be computed with ComputeSeries")
	default:
		return 0, fmt.Errorf("unknown formula type %q", f.Type)
	}
}

// leaf renders and runs one aggregate. Every identifier is validated against
// the whitelist and every literal goes through a bound parameter.
func (e *Engine) leaf(ctx context.Context, tenantID, periodCode string, f *Formula, dims map[string]string) (float64, error) {
	cols, ok := factColumns[f.Fact]
	if !ok {
		return 0, fmt.Errorf("unknown fact table %q", f.Fact)
	}
	asOf := f.AsOf
	if asOf == "" {
		asOf = "period_end"
	}
	if asOf != "period_end" && asOf != "latest" {
		return 0, fmt.Errorf("as_of must be period_end or latest (got %q)", asOf)
	}

	expr := "COUNT(*)"
	target := ""
	switch f.Type {
	case "count":
	case "count_distinct", "sum", "average":
		if f.Column == "" {
			return 0, fmt.Errorf("%s requires a column", f.Type)
		}
		target = f.Column
		if !cols[target] {
			return 0, fmt.Errorf("column %q is not aggregatable on %s", target, f.Fact)
		}
		switch f.Type {
		case "sum":
			expr = "SUM(" + target + ")"
		case "average":
			expr = "AVG(" + target + ")"
		case "count_distinct":
			expr = "COUNT(DISTINCT " + target + ")"
		}
	}

	where := []string{"tenant_id = $1"}
	args := []any{tenantID}

	// As-of anchor: the fact row set of the latest business_date on or before
	// the period end (the ETL writes one snapshot per business date).
	asOfClause := "business_date = (SELECT max(business_date) FROM " + f.Fact + " WHERE tenant_id = $1"
	if asOf == "period_end" && periodCode != "" {
		args = append(args, periodCode+"-01")
		asOfClause += " AND business_date <= ($" + itoa(len(args)) + "::date + INTERVAL '1 month' - INTERVAL '1 day')::date"
	}
	asOfClause += ")"
	where = append(where, asOfClause)

	// Formula filter (bound literals, whitelisted columns).
	for _, col := range sortedKeys(f.Filter) {
		if !cols[col] {
			return 0, fmt.Errorf("filter column %q does not exist on %s", col, f.Fact)
		}
		raw := f.Filter[col]
		// A comparison operator is expressed as {"col": {"op": ">", "value": "0"}};
		// a bare value keeps the equality/ANY form.
		if op, values, ok := comparisonFilter(raw); ok {
			if !validFilterOp(op) {
				return 0, fmt.Errorf("unsupported filter operator %q on %s", op, col)
			}
			args = append(args, values)
			if numericFactColumns[f.Fact][col] {
				where = append(where, fmt.Sprintf("%s::numeric %s ALL($%d::numeric[])",
					col, op, len(args)))
				continue
			}
			where = append(where, fmt.Sprintf("%s %s ALL($%d::text[])", col, op, len(args)))
			continue
		}
		values, err := jsonValues(raw)
		if err != nil {
			return 0, fmt.Errorf("filter %s: %w", col, err)
		}
		if len(values) == 0 {
			continue
		}
		args = append(args, values)
		where = append(where, fmt.Sprintf("%s = ANY($%d::text[])", eqExpr(f.Fact, col), len(args)))
	}

	// Dimension slicing from the request.
	dimsApplied := make([]string, 0, len(dims))
	for name := range dims {
		dimsApplied = append(dimsApplied, name)
	}
	sort.Strings(dimsApplied)
	for _, name := range dimsApplied {
		col, ok := dimDef[name]
		if !ok {
			return 0, fmt.Errorf("unknown dimension %q", name)
		}
		if !cols[col] {
			return 0, fmt.Errorf("dimension %q is not available on %s", name, f.Fact)
		}
		args = append(args, dims[name])
		where = append(where, fmt.Sprintf("%s = $%d::text", eqExpr(f.Fact, col), len(args)))
	}

	value, err := e.repo.ScalarQuery(ctx, "SELECT "+expr+" FROM "+f.Fact+" WHERE "+strings.Join(where, " AND "), args)
	if err != nil {
		return 0, fmt.Errorf("compute %s: %w", f.Type, err)
	}
	if f.Scale != 0 {
		value *= f.Scale
	}
	return value, nil
}

// ratioValue divides, guarding against a zero denominator: a ratio with no
// base is reported as 0 (never NaN/Inf, which numeric columns reject).
func ratioValue(num, den float64, percent bool) float64 {
	if den == 0 {
		return 0
	}
	v := num / den
	if percent {
		v *= 100
	}
	return v
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// jsonValues accepts either a single string or an array of strings.
func jsonValues(raw json.RawMessage) ([]string, error) {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return many, nil
	}
	return nil, fmt.Errorf("must be a string or array of strings")
}

// comparisonFilter recognises {"op": ">", "value": "0"} filters, which the
// membership indicators need (e.g. "members with capital above zero"). Numeric
// columns compare as numbers (see numericFactColumns); everything else stays
// the all-text filter convention the rest of the engine uses.
func comparisonFilter(raw json.RawMessage) (string, []string, bool) {
	var spec struct {
		Op    string          `json:"op"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil || spec.Op == "" || len(spec.Value) == 0 {
		return "", nil, false
	}
	values, err := jsonValues(spec.Value)
	if err != nil || len(values) == 0 {
		return "", nil, false
	}
	return spec.Op, values, true
}

// eqExpr renders an equality predicate operand. A numeric column (including an
// integer one like term_months) is cast to text so it can be compared against
// the text-bound literals the equality path uses; casting a column to text
// cannot change the WHERE semantics for equality. Non-numeric columns are used
// as-is.
func eqExpr(fact, col string) string {
	if numericFactColumns[fact][col] {
		return col + "::text"
	}
	return col
}

// validFilterOp is the closed set of comparison operators (never arbitrary
// SQL).
func validFilterOp(op string) bool {
	switch op {
	case ">", ">=", "<", "<=", "=", "<>":
		return true
	default:
		return false
	}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
