package reports

import (
	"fmt"
	"strings"
)

// P2.8b report query builders — parameterised, no free-form SQL (Q8).
// Each report has a builder function in this package with unit tests;
// rpt_report_definitions.query_id references one of these IDs. Parameters
// are validated against the definition's param schema by the handler before
// the builder runs.

// ParamError is a validation failure for report parameters.
type ParamError struct {
	Param string
	Msg   string
}

func (e *ParamError) Error() string {
	return fmt.Sprintf("param %q: %s", e.Param, e.Msg)
}

// ReportQuery is the rendered output of a builder: a fixed SQL statement
// plus validated, type-bound parameters. The SQL text NEVER contains
// caller input — callers only fill $n placeholders.
type ReportQuery struct {
	QueryID string
	SQL     string
	Args    []any
	Columns []string
}

// Params common to report builders.
type Params struct {
	TenantID   string
	PeriodCode string // "YYYY-MM"
	OrgCode    string // optional org scope filter
}

func validateCommon(p Params) error {
	if strings.TrimSpace(p.TenantID) == "" {
		return &ParamError{Param: "tenant_id", Msg: "required"}
	}
	if strings.TrimSpace(p.PeriodCode) == "" {
		return &ParamError{Param: "period_code", Msg: "required"}
	}
	if !validPeriod(p.PeriodCode) {
		return &ParamError{Param: "period_code", Msg: "must be YYYY-MM"}
	}
	return nil
}

func validPeriod(p string) bool {
	if len(p) != 7 || p[4] != '-' {
		return false
	}
	y, m := p[:4], p[5:]
	for _, ch := range y {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	if len(m) != 2 || m[0] < '0' || m[0] > '1' {
		return false
	}
	return true
}

// Registry of known query builders. rpt_report_definitions.query_id must
// match one of these IDs; anything else is rejected at the handler.
const (
	QueryLoanPortfolioSummary = "loan_portfolio_summary"
	QueryDepositPortfolio     = "deposit_portfolio"
)

var builders = map[string]func(Params) (*ReportQuery, error){
	QueryLoanPortfolioSummary: buildLoanPortfolioSummary,
	QueryDepositPortfolio:     buildDepositPortfolio,
}

// Build renders the named report query. Unknown query IDs fail closed.
func Build(queryID string, p Params) (*ReportQuery, error) {
	builder, ok := builders[queryID]
	if !ok {
		return nil, &ParamError{Param: "query_id", Msg: "unknown query: " + queryID}
	}
	if err := validateCommon(p); err != nil {
		return nil, err
	}
	return builder(p)
}

// KnownQueryIDs lists registered builder ids (for validation messages).
func KnownQueryIDs() []string {
	ids := make([]string, 0, len(builders))
	for id := range builders {
		ids = append(ids, id)
	}
	return ids
}

// buildLoanPortfolioSummary: outstanding theo debt group (from loan DB —
// the query runs on the loan database via the loan-service read API or the
// statistical job runner against the loan DSN; the SQL itself is fixed).
func buildLoanPortfolioSummary(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT debt_group_code,
       COUNT(*) AS agreement_count,
       SUM(outstanding_amt_minor) AS outstanding_minor
FROM lnm_agreements
WHERE tenant_id = $1
  AND status = 'ACTIVE'
  AND date_trunc('month', disburse_date) = ($2::date - INTERVAL '1 month + 1 day')::date
GROUP BY debt_group_code
ORDER BY debt_group_code`
	return &ReportQuery{
		QueryID: QueryLoanPortfolioSummary,
		SQL:     sqlText,
		Args:    []any{p.TenantID, "2026-" + p.PeriodCode[5:] + "-01"},
		Columns: []string{"debt_group_code", "agreement_count", "outstanding_minor"},
	}, nil
}

// buildDepositPortfolio: savings portfolio theo product.
func buildDepositPortfolio(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT product_code,
       COUNT(*) AS savings_count,
       SUM(principal_minor) AS principal_minor,
       SUM(accrued_minor) AS accrued_minor
FROM dpm_savings
WHERE tenant_id = $1
  AND status = 'ACTIVE'
GROUP BY product_code
ORDER BY product_code`
	return &ReportQuery{
		QueryID: QueryDepositPortfolio,
		SQL:     sqlText,
		Args:    []any{p.TenantID},
		Columns: []string{"product_code", "savings_count", "principal_minor", "accrued_minor"},
	}, nil
}
