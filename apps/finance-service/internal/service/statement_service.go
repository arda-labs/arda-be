package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// StatementService evaluates fin_statement_formula rows against
// fin_trial_balance_daily (P3a, fac-statistical-reporting-survey.md §5.2).
// Formulas are JSON references — never SQL (the EPAS
// fac_cfg_acct_formula/EXPRESSION_SQL pitfall, doc §4-4): accounts sum net
// balances of listed accounts, rows sum evaluated sibling lines, none marks
// a pure grouping header.
type StatementService struct {
	db *sql.DB
}

func NewStatementService(db *sql.DB) *StatementService {
	return &StatementService{db: db}
}

// formula is the JSONB shape stored in fin_statement_formula.formula.
type formula struct {
	Type    string          `json:"type"`
	Codes   []string        `json:"codes,omitempty"`   // accounts: plain list
	Members []formulaMember `json:"members,omitempty"` // rows: member + role sign
}

// formulaMember is one row reference inside a rows-formula. Sign is the
// member's accounting role within that total (+1 normal, -1 contra), on top
// of the member row's own display sign — the two are independent: 1319
// displays positive in both CDKT (contra asset) and B02 (expense).
type formulaMember struct {
	Code string `json:"code"`
	Sign int    `json:"sign,omitempty"` // 0 treated as +1
}

// StatementRow is one rendered statement line for the FE grid.
type StatementRow struct {
	RowCode     string `json:"row_code"`
	ParentCode  string `json:"parent_code,omitempty"`
	Label       string `json:"label"`
	Level       int    `json:"level"`
	SortOrder   int    `json:"sort_order"`
	IsTotal     bool   `json:"is_total"`
	AmountMinor int64  `json:"amount_minor"`
	HasAmount   bool   `json:"has_amount"`
}

// StatementResult is the rendered statement for one scope.
type StatementResult struct {
	TenantID      string         `json:"tenant_id"`
	StatementCode string         `json:"statement_code"`
	AsOf          string         `json:"as_of"`
	FromDate      string         `json:"from_date,omitempty"`
	CoaVersion    string         `json:"coa_version,omitempty"`
	Rows          []StatementRow `json:"rows"`
}

// RunStatement renders statementCode as of asOf from fin_trial_balance_daily.
// asOf empty = latest rebuilt date. fromDate bounds movement/opening formulas
// (B03 cash-flow); empty = first day of the month containing asOf. Rows come
// back in sort_order.
func (s *StatementService) RunStatement(ctx context.Context, tenantID, statementCode, asOf, coaVersion, fromDate string) (*StatementResult, error) {
	if statementCode == "" {
		return nil, fmt.Errorf("statement_code is required")
	}
	if asOf == "" {
		var err error
		asOf, err = s.latestRebuiltDate(ctx, tenantID)
		if err != nil {
			return nil, err
		}
	}
	asOfTime, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return nil, fmt.Errorf("as_of must be YYYY-MM-DD")
	}
	if fromDate == "" {
		fromDate = time.Date(asOfTime.Year(), asOfTime.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", fromDate); err != nil {
		return nil, fmt.Errorf("from must be YYYY-MM-DD")
	}
	if fromDate > asOf {
		return nil, fmt.Errorf("from must be on or before as_of")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT row_code, parent_code, label, level, sort_order, sign, formula, is_total
		FROM fin_statement_formula
		WHERE tenant_id = $1 AND statement_code = $2
		ORDER BY sort_order, row_code`, tenantID, statementCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	defs := []statementDef{}
	for rows.Next() {
		var d statementDef
		var parent sql.NullString
		var raw []byte
		if err := rows.Scan(&d.RowCode, &parent, &d.Label, &d.Level, &d.SortOrder,
			&d.Sign, &raw, &d.IsTotal); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &d.Formula); err != nil {
			return nil, fmt.Errorf("row %s: bad formula json: %w", d.RowCode, err)
		}
		if parent.Valid {
			d.ParentCode = parent.String
		}
		defs = append(defs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, fmt.Errorf("statement %q has no rows for tenant", statementCode)
	}

	// Net debit-positive balance per account as of asOf (opening booked
	// before asOf + all movements up to asOf); opening at period start and
	// period movement (incr_*) for B03 cash-flow rows.
	acctBal, err := s.accountBalances(ctx, tenantID, asOf, coaVersion)
	if err != nil {
		return nil, err
	}
	openBal, err := s.accountOpeningBalances(ctx, tenantID, fromDate, coaVersion)
	if err != nil {
		return nil, err
	}
	movement, err := s.accountMovements(ctx, tenantID, fromDate, asOf, coaVersion)
	if err != nil {
		return nil, err
	}

	result, err := evaluateStatement(defs, statementInputs{
		asOf:     acctBal,
		opening:  openBal,
		movement: movement,
	})
	if err != nil {
		return nil, err
	}
	result.TenantID = tenantID
	result.StatementCode = statementCode
	result.AsOf = asOf
	result.FromDate = fromDate
	if result.CoaVersion == "" {
		result.CoaVersion = coaVersion
	}
	sort.SliceStable(result.Rows, func(i, j int) bool {
		return result.Rows[i].SortOrder < result.Rows[j].SortOrder
	})
	return result, nil
}

// statementDef is one fin_statement_formula row handed to the evaluator.
type statementDef struct {
	RowCode    string
	ParentCode string
	Label      string
	Level      int
	SortOrder  int
	Sign       int
	Formula    formula
	IsTotal    bool
}

// statementInputs carries the per-account maps the evaluator reads. asOf is
// the standing net balance at as_of; opening is the standing net balance
// before from_date; movement is the period movement (incr debit - incr credit)
// between from_date and as_of.
type statementInputs struct {
	asOf     map[string]int64
	opening  map[string]int64
	movement map[string]int64
}

// evaluateStatement renders rows from definitions + per-account net
// balances. Pure function — DB I/O stays in RunStatement.
//
// Two independent sign layers (the classic statement-engine split):
//   - row Sign = presentation: AmountMinor = expression sum × Sign, so a
//     credit-nature account (1319 dự phòng, 511x doanh thu) displays as a
//     positive figure with Sign=-1;
//   - rows-formula member Sign = accounting role within that total (+1
//     normal, -1 contra): the member's DISPLAY value enters with this sign.
//     1319 is contra (-1) inside CDKT Tổng tài sản but a normal expense
//     member (+1) inside B02 Tổng chi phí.
//
// Rows evaluate in definition order (seed sort_order); a total must come
// after the rows it references.
func evaluateStatement(defs []statementDef, in statementInputs) (*StatementResult, error) {
	result := &StatementResult{Rows: make([]StatementRow, 0, len(defs))}
	valueOf := map[string]int64{} // display value of each evaluated row
	for _, d := range defs {
		row := StatementRow{
			RowCode: d.RowCode, Label: d.Label, Level: d.Level,
			SortOrder: d.SortOrder, IsTotal: d.IsTotal, ParentCode: d.ParentCode,
		}
		var sum int64
		hasAmount := false
		switch d.Formula.Type {
		case "accounts":
			for _, code := range d.Formula.Codes {
				sum += in.asOf[code]
			}
			hasAmount = true
		case "opening":
			for _, code := range d.Formula.Codes {
				sum += in.opening[code]
			}
			hasAmount = true
		case "movement":
			for _, code := range d.Formula.Codes {
				sum += in.movement[code]
			}
			hasAmount = true
		case "rows":
			for _, m := range d.Formula.Members {
				sign := int64(1)
				if m.Sign != 0 {
					sign = int64(m.Sign)
				}
				sum += sign * valueOf[m.Code]
			}
			hasAmount = true
		case "none", "":
			// grouping row
		default:
			return nil, fmt.Errorf("row %s: unknown formula type %q", d.RowCode, d.Formula.Type)
		}
		display := sum * int64(d.Sign)
		valueOf[d.RowCode] = display
		row.AmountMinor = display
		row.HasAmount = hasAmount
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

// StatementSummary is one selectable statement for the FE menu.
type StatementSummary struct {
	StatementCode string `json:"statement_code"`
	RowCount      int    `json:"row_count"`
}

// FinancialSummary consolidates the key totals of the balance sheet (CDKT) and
// operating results (B02) — the Tổng hợp báo cáo tài chính screen.
type FinancialSummary struct {
	AsOf                  string `json:"as_of"`
	FromDate              string `json:"from_date,omitempty"`
	TotalAssetsMinor      int64  `json:"total_assets_minor"`
	TotalLiabilitiesMinor int64  `json:"total_liabilities_minor"`
	TotalEquityMinor      int64  `json:"total_equity_minor"`
	TotalIncomeMinor      int64  `json:"total_income_minor"`
	TotalExpenseMinor     int64  `json:"total_expense_minor"`
	ProfitMinor           int64  `json:"profit_minor"`
}

// FinancialSummary runs CDKT + B02 and returns their total rows.
func (s *StatementService) FinancialSummary(ctx context.Context, tenantID, asOf, coaVersion, fromDate string) (*FinancialSummary, error) {
	cdkt, err := s.RunStatement(ctx, tenantID, "CDKT", asOf, coaVersion, fromDate)
	if err != nil {
		return nil, err
	}
	b02, err := s.RunStatement(ctx, tenantID, "B02", asOf, coaVersion, fromDate)
	if err != nil {
		return nil, err
	}
	return &FinancialSummary{
		AsOf:                  cdkt.AsOf,
		FromDate:              cdkt.FromDate,
		TotalAssetsMinor:      statementRowAmount(cdkt, "ASSETS_TOTAL"),
		TotalLiabilitiesMinor: statementRowAmount(cdkt, "LIAB_TOTAL"),
		TotalEquityMinor:      statementRowAmount(cdkt, "EQUITY_TOTAL"),
		TotalIncomeMinor:      statementRowAmount(b02, "INCOME_TOTAL"),
		TotalExpenseMinor:     statementRowAmount(b02, "EXPENSE_TOTAL"),
		ProfitMinor:           statementRowAmount(b02, "PROFIT"),
	}, nil
}

func statementRowAmount(res *StatementResult, rowCode string) int64 {
	for _, row := range res.Rows {
		if row.RowCode == rowCode {
			return row.AmountMinor
		}
	}
	return 0
}

func (s *StatementService) ListStatements(ctx context.Context, tenantID string) ([]StatementSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT statement_code, count(*) AS row_count
		FROM fin_statement_formula
		WHERE tenant_id = $1
		GROUP BY statement_code
		ORDER BY statement_code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []StatementSummary{}
	for rows.Next() {
		var st StatementSummary
		if err := rows.Scan(&st.StatementCode, &st.RowCount); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// latestRebuiltDate returns max(business_date) present in the precompute.
func (s *StatementService) latestRebuiltDate(ctx context.Context, tenantID string) (string, error) {
	var d sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT max(business_date)::text FROM fin_trial_balance_daily WHERE tenant_id = $1`, tenantID).Scan(&d)
	if err != nil {
		return "", err
	}
	if !d.Valid {
		return "", fmt.Errorf("no trial balance rebuilt yet — run the COB trial-balance step first")
	}
	return d.String, nil
}

// accountBalances returns net debit-positive balance per account_code as of
// asOf: sum of all daily close deltas up to asOf (last close ≤ asOf per
// account + movement on dates without rebuild). Because the COB rebuilds
// every day contiguously, taking each account's latest close ≤ asOf is the
// correct standing balance.
func (s *StatementService) accountBalances(ctx context.Context, tenantID, asOf, coaVersion string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (tbd.coa_version, tbd.account_code, tbd.currency_code)
		       tbd.coa_version, tbd.account_code,
		       tbd.close_debit_minor - tbd.close_credit_minor AS net_minor
		FROM fin_trial_balance_daily tbd
		WHERE tbd.tenant_id = $1 AND tbd.business_date <= $2::date
		ORDER BY tbd.coa_version, tbd.account_code, tbd.currency_code, tbd.business_date DESC`,
		tenantID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bal := map[string]int64{}
	for rows.Next() {
		var v, code string
		var net int64
		if err := rows.Scan(&v, &code, &net); err != nil {
			return nil, err
		}
		if coaVersion != "" && v != coaVersion {
			continue
		}
		bal[code] += net
	}
	return bal, rows.Err()
}

// accountOpeningBalances returns the standing net balance immediately before
// fromDate (business_date < from_date) — the opening balance of a period.
func (s *StatementService) accountOpeningBalances(ctx context.Context, tenantID, fromDate, coaVersion string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (tbd.coa_version, tbd.account_code, tbd.currency_code)
		       tbd.coa_version, tbd.account_code,
		       tbd.close_debit_minor - tbd.close_credit_minor AS net_minor
		FROM fin_trial_balance_daily tbd
		WHERE tbd.tenant_id = $1 AND tbd.business_date < $2::date
		ORDER BY tbd.coa_version, tbd.account_code, tbd.currency_code, tbd.business_date DESC`,
		tenantID, fromDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bal := map[string]int64{}
	for rows.Next() {
		var v, code string
		var net int64
		if err := rows.Scan(&v, &code, &net); err != nil {
			return nil, err
		}
		if coaVersion != "" && v != coaVersion {
			continue
		}
		bal[code] += net
	}
	return bal, rows.Err()
}

// accountMovements sums period movement (incr debit - incr credit) per
// account_code over [fromDate, asOf] — the input for B03 cash-flow rows.
func (s *StatementService) accountMovements(ctx context.Context, tenantID, fromDate, asOf, coaVersion string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tbd.coa_version, tbd.account_code,
		       SUM(tbd.incr_debit_minor - tbd.incr_credit_minor) AS net_minor
		FROM fin_trial_balance_daily tbd
		WHERE tbd.tenant_id = $1
		  AND tbd.business_date >= $2::date AND tbd.business_date <= $3::date
		  AND ($4 = '' OR tbd.coa_version = $4)
		GROUP BY tbd.coa_version, tbd.account_code`,
		tenantID, fromDate, asOf, coaVersion)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bal := map[string]int64{}
	for rows.Next() {
		var v, code string
		var net int64
		if err := rows.Scan(&v, &code, &net); err != nil {
			return nil, err
		}
		if coaVersion != "" && v != coaVersion {
			continue
		}
		bal[code] += net
	}
	return bal, rows.Err()
}
