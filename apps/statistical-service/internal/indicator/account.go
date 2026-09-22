package indicator

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// accountBalance is the aggregate behind the PCF "Tài chính kế toán"
// indicators. Each of those is a signed sum of end-of-day account balances:
// DCN TK 10 (cash), DCC TK 139 (provision), DCN TK 30 - DCC TK 305 (fixed
// assets net), or "Tổng DCN các TK (10, 11, …) - Tổng DCC các TK (139, …) +
// (DCN TK 5 - DCC TK 5) nếu DCN - DCC > 0".
//
// The formula is expressed as a list of SIGNED TERMS over account-code
// prefixes, which is what the source formulas actually are. It stays a closed
// whitelist: account codes are validated as bare digit prefixes and every value
// travels as a bound parameter, so this is not SQL-as-config.
type accountBalance struct {
	Fact  string        `json:"fact"`
	Terms []accountTerm `json:"terms"`
	AsOf  string        `json:"as_of,omitempty"`
	Scale float64       `json:"scale,omitempty"`
}

// accountTerm is one signed component. Side selects which balance column(s) the
// prefixes are summed over:
//
//	debit  -> SUM(close_debit_minor)
//	credit -> SUM(close_credit_minor)
//	net    -> SUM(close_debit_minor) - SUM(close_credit_minor)
//
// Sign multiplies the term (-1 for a subtracted component), and Clamp confines
// a term to one side of zero ("positive" renders the "nếu DN - DCC > 0"
// condition used by the total-asset formula).
type accountTerm struct {
	Side     string   `json:"side"`
	Prefixes []string `json:"prefixes"`
	// Exclude narrows the prefixes by account codes to skip, e.g.
	// "36 (trừ TK 366)" -> include 36*, exclude 366*.
	Exclude []string `json:"exclude,omitempty"`
	Sign    *int     `json:"sign,omitempty"`
	Clamp   string   `json:"clamp,omitempty"`
}

// accountPrefixRe is the only shape an account prefix may take. It is applied
// to every entry before the value reaches SQL.
var accountPrefixRe = regexp.MustCompile(`^[0-9]{1,6}$`)

// maxAccountPrefixes bounds the work one indicator can ask for; the real
// catalog tops out around forty in the total-asset formula.
const maxAccountPrefixes = 256

// maxAccountTerms bounds the number of signed components.
const maxAccountTerms = 32

func (a *accountBalance) validate() error {
	if strings.TrimSpace(a.Fact) == "" {
		return fmt.Errorf("account_balance requires a fact")
	}
	if _, ok := factColumns[a.Fact]; !ok {
		return fmt.Errorf("unknown fact table %q", a.Fact)
	}
	if !colsHaveCloseBalances(a.Fact) {
		return fmt.Errorf("fact %q has no close_debit_minor/close_credit_minor columns", a.Fact)
	}
	if len(a.Terms) == 0 {
		return fmt.Errorf("account_balance requires at least one term")
	}
	if len(a.Terms) > maxAccountTerms {
		return fmt.Errorf("account_balance accepts at most %d terms", maxAccountTerms)
	}
	total := 0
	for i, term := range a.Terms {
		switch term.Side {
		case "debit", "credit", "net":
		default:
			return fmt.Errorf("term %d: side must be debit, credit or net (got %q)", i, term.Side)
		}
		if len(term.Prefixes) == 0 {
			return fmt.Errorf("term %d: at least one prefix is required", i)
		}
		total += len(term.Prefixes)
		for _, p := range term.Prefixes {
			if !accountPrefixRe.MatchString(p) {
				return fmt.Errorf("term %d: invalid account prefix %q (digits only)", i, p)
			}
		}
		for _, x := range term.Exclude {
			if !accountPrefixRe.MatchString(x) {
				return fmt.Errorf("term %d: invalid excluded prefix %q (digits only)", i, x)
			}
		}
		switch term.Clamp {
		case "", "positive", "negative":
		default:
			return fmt.Errorf("term %d: clamp must be empty, positive or negative (got %q)", i, term.Clamp)
		}
		if term.Sign != nil && *term.Sign != 1 && *term.Sign != -1 {
			return fmt.Errorf("term %d: sign must be 1 or -1", i)
		}
	}
	if total > maxAccountPrefixes {
		return fmt.Errorf("account_balance accepts at most %d prefixes", maxAccountPrefixes)
	}
	return nil
}

// evalAccountBalance renders and runs the signed sum.
func (e *Engine) evalAccountBalance(ctx context.Context, tenantID, periodCode string, a *accountBalance, dims map[string]string) (float64, error) {
	if err := a.validate(); err != nil {
		return 0, err
	}
	cols, _ := factColumns[a.Fact]

	where := []string{"tenant_id = $1"}
	args := []any{tenantID}

	asOf := a.AsOf
	if asOf == "" {
		asOf = "period_end"
	}
	if asOf != "period_end" && asOf != "latest" {
		return 0, fmt.Errorf("as_of must be period_end or latest (got %q)", asOf)
	}
	asOfClause := "business_date = (SELECT max(business_date) FROM " + a.Fact + " WHERE tenant_id = $1"
	if asOf == "period_end" && periodCode != "" {
		args = append(args, periodCode+"-01")
		asOfClause += " AND business_date <= ($" + itoa(len(args)) + "::date + INTERVAL '1 month' - INTERVAL '1 day')::date"
	}
	asOfClause += ")"
	where = append(where, asOfClause)

	dimNames := make([]string, 0, len(dims))
	for name := range dims {
		dimNames = append(dimNames, name)
	}
	sort.Strings(dimNames)
	for _, name := range dimNames {
		col, ok := dimDef[name]
		if !ok {
			return 0, fmt.Errorf("unknown dimension %q", name)
		}
		if !cols[col] {
			return 0, fmt.Errorf("dimension %q is not available on %s", name, a.Fact)
		}
		args = append(args, dims[name])
		where = append(where, fmt.Sprintf("%s = $%d::text", eqExpr(a.Fact, col), len(args)))
	}

	parts, args, err := a.termsExpr(args)
	if err != nil {
		return 0, err
	}

	sqlText := "SELECT (0" + strings.Join(parts, "") + ")::numeric FROM " + a.Fact + " WHERE " + strings.Join(where, " AND ")
	value, err := e.repo.ScalarQuery(ctx, sqlText, args)
	if err != nil {
		return 0, fmt.Errorf("compute account_balance: %w", err)
	}
	if a.Scale != 0 {
		value *= a.Scale
	}
	return value, nil
}

// termsExpr renders "(coalesce(...) [* -1]) [clamped]" per term, each prefixed
// with its sign so the caller can concatenate them after a leading 0.
func (a *accountBalance) termsExpr(args []any) ([]string, []any, error) {
	out := make([]string, 0, len(a.Terms))
	for _, term := range a.Terms {
		prefixClause := make([]string, 0, len(term.Prefixes)+len(term.Exclude))
		for _, p := range term.Prefixes {
			args = append(args, p+"%")
			prefixClause = append(prefixClause, "account_code LIKE $"+itoa(len(args)))
		}
		wherePrefix := "(" + strings.Join(prefixClause, " OR ") + ")"
		for _, x := range term.Exclude {
			args = append(args, x+"%")
			wherePrefix += " AND account_code NOT LIKE $" + itoa(len(args))
		}

		debit := "COALESCE(SUM(close_debit_minor) FILTER (WHERE " + wherePrefix + "), 0)"
		credit := "COALESCE(SUM(close_credit_minor) FILTER (WHERE " + wherePrefix + "), 0)"

		var body string
		switch term.Side {
		case "debit":
			body = debit
		case "credit":
			body = credit
		case "net":
			body = "(" + debit + " - " + credit + ")"
		}
		switch term.Clamp {
		case "positive":
			body = "GREATEST(" + body + ", 0)"
		case "negative":
			body = "LEAST(" + body + ", 0)"
		}

		sign := "+"
		if term.Sign != nil && *term.Sign == -1 {
			sign = "-"
		}
		out = append(out, " "+sign+" "+body)
	}
	return out, args, nil
}

// colsHaveCloseBalances reports whether a fact carries the trial-balance shape
// this aggregate needs.
func colsHaveCloseBalances(fact string) bool {
	cols := factColumns[fact]
	return cols["close_debit_minor"] && cols["close_credit_minor"] && cols["account_code"]
}
