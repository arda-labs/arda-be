package reports

import (
	"strings"
	"testing"
)

func TestBuildLoanPortfolioSummary(t *testing.T) {
	q, err := Build(QueryLoanPortfolioSummary, Params{TenantID: "t1", PeriodCode: "2026-09"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if q.QueryID != QueryLoanPortfolioSummary {
		t.Fatalf("query_id = %q", q.QueryID)
	}
	if len(q.Args) != 2 || q.Args[0] != "t1" {
		t.Fatalf("args mismatch: %v", q.Args)
	}
	// The tenant id must be a bound parameter, never embedded in SQL text.
	if strings.Contains(q.SQL, "t1") {
		t.Fatal("tenant id leaked into SQL text")
	}
	if len(q.Columns) != 3 {
		t.Fatalf("columns = %v", q.Columns)
	}
}

func TestBuildRejectsUnknownQueryID(t *testing.T) {
	if _, err := Build("DROP TABLE users; --", Params{TenantID: "t1", PeriodCode: "2026-09"}); err == nil {
		t.Fatal("unknown query id must fail closed")
	}
}

func TestBuildRPTSet(t *testing.T) {
	cases := []struct {
		id      string
		args    int
		columns int
	}{
		{QueryLoanAppraisalSummary, 2, 5},
		{QueryLoanDebtClassification, 1, 5},
		{QueryCustomerSummary, 1, 3},
		{QueryOperationControl, 2, 4},
		{QueryDepositMaturityLadder, 1, 4},
		{QueryDepositAccruedByProduct, 1, 4},
		{QueryCapitalByFundType, 1, 4},
		{QueryCapitalMovements, 2, 4},
		{QueryCollateralByType, 1, 4},
	}
	for _, tc := range cases {
		q, err := Build(tc.id, Params{TenantID: "t1", PeriodCode: "2026-09"})
		if err != nil {
			t.Fatalf("build %s: %v", tc.id, err)
		}
		if len(q.Args) != tc.args {
			t.Fatalf("%s: args = %v, want %d bound args", tc.id, q.Args, tc.args)
		}
		if len(q.Columns) != tc.columns {
			t.Fatalf("%s: columns = %v, want %d", tc.id, q.Columns, tc.columns)
		}
		if !strings.Contains(q.SQL, "WHERE") {
			t.Fatalf("%s: query must be tenant-scoped", tc.id)
		}
	}
}

func TestBuildValidatesParams(t *testing.T) {
	cases := []Params{
		{TenantID: "", PeriodCode: "2026-09"},
		{TenantID: "t1", PeriodCode: ""},
		{TenantID: "t1", PeriodCode: "202609"},
		{TenantID: "t1", PeriodCode: "2026-13-01"},
	}
	for i, p := range cases {
		if _, err := Build(QueryDepositPortfolio, p); err == nil {
			t.Fatalf("case %d: expected validation error for %+v", i, p)
		}
	}
}

func TestValidPeriod(t *testing.T) {
	valid := []string{"2026-01", "2026-09", "2026-12", "1999-11"}
	for _, p := range valid {
		if !validPeriod(p) {
			t.Fatalf("validPeriod(%q) = false, want true", p)
		}
	}
	invalid := []string{
		"", "2026", "202609", "2026-1", "2026-9", "2026-00", "2026-13",
		"2026-19", "2026-99", "26-01", "2026-01-01", "abcd-01",
		"2026-0a", "2026-01 ", " 2026-01", "2026/01",
	}
	for _, p := range invalid {
		if validPeriod(p) {
			t.Fatalf("validPeriod(%q) = true, want false", p)
		}
	}
}

// The dư nợ report must be a period-end snapshot, not a previous-month
// disbursement flow.
func TestBuildLoanPortfolioSummaryUsesPeriodEnd(t *testing.T) {
	q, err := Build(QueryLoanPortfolioSummary, Params{TenantID: "t1", PeriodCode: "2026-09"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(q.SQL, "disburse_date <= ($2::date + INTERVAL '1 month' - INTERVAL '1 day')") {
		t.Fatalf("portfolio summary must cap disbursement at period end:\n%s", q.SQL)
	}
	if strings.Contains(q.SQL, "date_trunc('month', disburse_date)") {
		t.Fatalf("portfolio summary still filters a single disbursement month:\n%s", q.SQL)
	}
	if len(q.Args) != 2 {
		t.Fatalf("args = %v, want tenant + period start", q.Args)
	}
}

// Overdue count must reflect loans past maturity at run time, not loans that
// will merely mature during the reporting period.
func TestBuildLoanDebtClassificationOverdueIsActual(t *testing.T) {
	q, err := Build(QueryLoanDebtClassification, Params{TenantID: "t1", PeriodCode: "2026-09"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.Contains(q.SQL, "maturity_date < CURRENT_DATE") {
		t.Fatalf("overdue must compare maturity_date to CURRENT_DATE:\n%s", q.SQL)
	}
	if strings.Contains(q.SQL, "INTERVAL '1 month'") {
		t.Fatalf("overdue must not compare against the reporting period end:\n%s", q.SQL)
	}
	if len(q.Args) != 1 || q.Args[0] != "t1" {
		t.Fatalf("args = %v, want [t1] (period is not used by this builder)", q.Args)
	}
}

func TestSQLNeverEmbedsParams(t *testing.T) {
	for _, id := range KnownQueryIDs() {
		q, err := Build(id, Params{TenantID: "tenant-with-quotes'; DROP", PeriodCode: "2026-09"})
		if err != nil {
			t.Fatalf("build %s: %v", id, err)
		}
		if strings.Contains(q.SQL, "tenant-with-quotes") {
			t.Fatalf("query %s embeds caller input in SQL text", id)
		}
		if strings.Contains(q.SQL, "';") {
			t.Fatalf("query %s contains statement-breaking sequence", id)
		}
	}
}
