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
	QueryLoanPortfolioSummary    = "loan_portfolio_summary"
	QueryDepositPortfolio        = "deposit_portfolio"
	QueryLoanAppraisalSummary    = "loan_appraisal_summary"
	QueryLoanDebtClassification  = "loan_debt_classification"
	QueryCustomerSummary         = "customer_summary"
	QueryOperationControl        = "operation_control_summary"
	QueryDepositMaturityLadder   = "deposit_maturity_ladder"
	QueryDepositAccruedByProduct = "deposit_accrued_by_product"
	QueryCapitalByFundType       = "capital_by_fund_type"
	QueryCapitalMovements        = "capital_movements"
	QueryCollateralByType        = "collateral_by_type"
)

var builders = map[string]func(Params) (*ReportQuery, error){
	QueryLoanPortfolioSummary:    buildLoanPortfolioSummary,
	QueryDepositPortfolio:        buildDepositPortfolio,
	QueryLoanAppraisalSummary:    buildLoanAppraisalSummary,
	QueryLoanDebtClassification:  buildLoanDebtClassification,
	QueryCustomerSummary:         buildCustomerSummary,
	QueryOperationControl:        buildOperationControl,
	QueryDepositMaturityLadder:   buildDepositMaturityLadder,
	QueryDepositAccruedByProduct: buildDepositAccruedByProduct,
	QueryCapitalByFundType:       buildCapitalByFundType,
	QueryCapitalMovements:        buildCapitalMovements,
	QueryCollateralByType:        buildCollateralByType,
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
  AND date_trunc('month', disburse_date) = date_trunc('month', ($2::date - INTERVAL '1 month'))
GROUP BY debt_group_code
ORDER BY debt_group_code`
	return &ReportQuery{
		QueryID: QueryLoanPortfolioSummary,
		SQL:     sqlText,
		Args:    []any{p.TenantID, p.PeriodCode + "-01"},
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

// buildLoanAppraisalSummary (rpt-loan-appraisal): số hồ sơ đã thẩm định/giải
// ngân trong kỳ, dư nợ và lãi suất bình quân — tổng hợp theo tháng.
func buildLoanAppraisalSummary(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT to_char(date_trunc('month', disburse_date), 'YYYY-MM') AS period_code,
       COUNT(*) AS agreement_count,
       SUM(disburse_amt_minor) AS disburse_minor,
       ROUND(AVG(interest_rate)::numeric, 4) AS avg_interest_rate,
       SUM(outstanding_amt_minor) AS outstanding_minor
FROM lnm_agreements
WHERE tenant_id = $1
  AND disburse_date IS NOT NULL
  AND date_trunc('month', disburse_date) = date_trunc('month', $2::date)
GROUP BY 1
ORDER BY 1`
	return &ReportQuery{
		QueryID: QueryLoanAppraisalSummary,
		SQL:     sqlText,
		Args:    []any{p.TenantID, p.PeriodCode + "-01"},
		Columns: []string{"period_code", "agreement_count", "disburse_minor", "avg_interest_rate", "outstanding_minor"},
	}, nil
}

// buildLoanDebtClassification (rpt-loan-classification): phân loại nợ theo
// nhóm, kèm dư nợ, dự phòng và số khoản quá hạn (maturity_date < cuối kỳ).
func buildLoanDebtClassification(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT debt_group_code,
       COUNT(*) AS agreement_count,
       SUM(outstanding_amt_minor) AS outstanding_minor,
       SUM(provision_amt_minor) AS provision_minor,
       SUM(CASE WHEN maturity_date IS NOT NULL AND maturity_date < ($2::date + INTERVAL '1 month') THEN 1 ELSE 0 END) AS overdue_count
FROM lnm_agreements
WHERE tenant_id = $1
  AND status = 'ACTIVE'
GROUP BY debt_group_code
ORDER BY debt_group_code`
	return &ReportQuery{
		QueryID: QueryLoanDebtClassification,
		SQL:     sqlText,
		Args:    []any{p.TenantID, p.PeriodCode + "-01"},
		Columns: []string{"debt_group_code", "agreement_count", "outstanding_minor", "provision_minor", "overdue_count"},
	}, nil
}

// buildCustomerSummary (rpt-customer): khách hàng theo phân khúc + trạng thái.
func buildCustomerSummary(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT COALESCE(NULLIF(segment, ''), 'UNSEGMENTED') AS segment,
       status,
       COUNT(*) AS customer_count
FROM customers
WHERE tenant_id = $1
GROUP BY 1, 2
ORDER BY 1, 2`
	return &ReportQuery{
		QueryID: QueryCustomerSummary,
		SQL:     sqlText,
		Args:    []any{p.TenantID},
		Columns: []string{"segment", "status", "customer_count"},
	}, nil
}

// buildOperationControl (rpt-operation-control): kiểm soát vận hành báo cáo —
// tình trạng nộp báo cáo theo kỳ (chạy trên chính statistical DB).
func buildOperationControl(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT report_code,
       period_code,
       status,
       COUNT(*) AS submission_count
FROM rpt_report_submissions
WHERE tenant_id = $1
  AND period_code = $2
GROUP BY report_code, period_code, status
ORDER BY report_code, status`
	return &ReportQuery{
		QueryID: QueryOperationControl,
		SQL:     sqlText,
		Args:    []any{p.TenantID, p.PeriodCode},
		Columns: []string{"report_code", "period_code", "status", "submission_count"},
	}, nil
}

// buildDepositMaturityLadder: huy động đang hoạt động theo tháng đáo hạn.
func buildDepositMaturityLadder(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT to_char(date_trunc('month', maturity_date), 'YYYY-MM') AS maturity_month,
       COUNT(*) AS savings_count,
       SUM(principal_minor) AS principal_minor,
       SUM(accrued_minor) AS accrued_minor
FROM dpm_savings
WHERE tenant_id = $1 AND status = 'ACTIVE'
GROUP BY 1
ORDER BY 1`
	return &ReportQuery{
		QueryID: QueryDepositMaturityLadder,
		SQL:     sqlText,
		Args:    []any{p.TenantID},
		Columns: []string{"maturity_month", "savings_count", "principal_minor", "accrued_minor"},
	}, nil
}

// buildDepositAccruedByProduct: lãi dự chi theo sản phẩm huy động.
func buildDepositAccruedByProduct(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT product_code,
       COUNT(*) AS savings_count,
       SUM(principal_minor) AS principal_minor,
       SUM(accrued_minor) AS accrued_minor
FROM dpm_savings
WHERE tenant_id = $1 AND status = 'ACTIVE'
GROUP BY product_code
ORDER BY product_code`
	return &ReportQuery{
		QueryID: QueryDepositAccruedByProduct,
		SQL:     sqlText,
		Args:    []any{p.TenantID},
		Columns: []string{"product_code", "savings_count", "principal_minor", "accrued_minor"},
	}, nil
}

// buildCapitalByFundType: nguồn vốn theo loại quỹ.
func buildCapitalByFundType(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT fund_type_code,
       COUNT(*) AS contract_count,
       SUM(amount_minor) AS amount_minor,
       ROUND(AVG(interest_rate)::numeric, 4) AS avg_interest_rate
FROM cfc_contracts
WHERE tenant_id = $1 AND status = 'ACTIVE'
GROUP BY fund_type_code
ORDER BY fund_type_code`
	return &ReportQuery{
		QueryID: QueryCapitalByFundType,
		SQL:     sqlText,
		Args:    []any{p.TenantID},
		Columns: []string{"fund_type_code", "contract_count", "amount_minor", "avg_interest_rate"},
	}, nil
}

// buildCapitalMovements: biến động nguồn vốn theo loại trong kỳ.
func buildCapitalMovements(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT movement_type,
       to_char(date_trunc('month', movement_date), 'YYYY-MM') AS period_code,
       COUNT(*) AS movement_count,
       SUM(amount_minor) AS amount_minor
FROM cfc_movements
WHERE tenant_id = $1
  AND movement_date >= $2::date
  AND movement_date < ($2::date + INTERVAL '1 month')
GROUP BY movement_type, 2
ORDER BY 2, movement_type`
	return &ReportQuery{
		QueryID: QueryCapitalMovements,
		SQL:     sqlText,
		Args:    []any{p.TenantID, p.PeriodCode + "-01"},
		Columns: []string{"movement_type", "period_code", "movement_count", "amount_minor"},
	}, nil
}

// buildCollateralByType: tài sản bảo đảm theo loại.
func buildCollateralByType(p Params) (*ReportQuery, error) {
	sqlText := `
SELECT COALESCE(NULLIF(coll_type_code, ''), 'UNCLASSIFIED') AS coll_type_code,
       COUNT(*) AS collateral_count,
       SUM(coll_value_minor) AS coll_value_minor,
       SUM(coll_use_value_minor) AS coll_use_value_minor
FROM lnm_collaterals
WHERE tenant_id = $1
GROUP BY 1
ORDER BY 1`
	return &ReportQuery{
		QueryID: QueryCollateralByType,
		SQL:     sqlText,
		Args:    []any{p.TenantID},
		Columns: []string{"coll_type_code", "collateral_count", "coll_value_minor", "coll_use_value_minor"},
	}, nil
}
