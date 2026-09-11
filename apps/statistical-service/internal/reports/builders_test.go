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
		{QueryLoanDebtClassification, 2, 5},
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
