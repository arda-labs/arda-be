package indicator

import (
	"strings"
	"testing"
)

func sign(v int) *int { return &v }

// TestAccountBalanceValidation keeps the account surface closed: only digit
// prefixes, only the trial-balance fact, and clamps/signs confined to the known
// sets.
func TestAccountBalanceValidation(t *testing.T) {
	cases := []struct {
		name string
		ab   accountBalance
	}{
		{"unknown fact", accountBalance{Fact: "pg_shadow", Terms: []accountTerm{{Side: "debit", Prefixes: []string{"10"}}}}},
		{"fact without close balances", accountBalance{Fact: "rpt_fact_customer_daily", Terms: []accountTerm{{Side: "debit", Prefixes: []string{"10"}}}}},
		{"no terms", accountBalance{Fact: "rpt_fact_trial_balance_daily"}},
		{"empty prefixes", accountBalance{Fact: "rpt_fact_trial_balance_daily", Terms: []accountTerm{{Side: "debit"}}}},
		{"injection in prefix", accountBalance{Fact: "rpt_fact_trial_balance_daily", Terms: []accountTerm{{Side: "debit", Prefixes: []string{"10%' OR 1=1 --"}}}}},
		{"non numeric prefix", accountBalance{Fact: "rpt_fact_trial_balance_daily", Terms: []accountTerm{{Side: "debit", Prefixes: []string{"TK10"}}}}},
		{"bad side", accountBalance{Fact: "rpt_fact_trial_balance_daily", Terms: []accountTerm{{Side: "both", Prefixes: []string{"10"}}}}},
		{"bad clamp", accountBalance{Fact: "rpt_fact_trial_balance_daily", Terms: []accountTerm{{Side: "debit", Prefixes: []string{"10"}, Clamp: "maybe"}}}},
		{"bad sign", accountBalance{Fact: "rpt_fact_trial_balance_daily", Terms: []accountTerm{{Side: "debit", Prefixes: []string{"10"}, Sign: sign(2)}}}},
	}
	for _, c := range cases {
		if err := c.ab.validate(); err == nil {
			t.Fatalf("%s: expected validation failure", c.name)
		}
	}

	ok := accountBalance{
		Fact: "rpt_fact_trial_balance_daily",
		Terms: []accountTerm{
			{Side: "debit", Prefixes: []string{"10", "11"}},
			{Side: "credit", Prefixes: []string{"139"}, Sign: sign(-1)},
			{Side: "net", Prefixes: []string{"5"}, Clamp: "positive"},
		},
	}
	if err := ok.validate(); err != nil {
		t.Fatalf("valid account balance rejected: %v", err)
	}
}

// TestAccountBalanceTermsBindPrefixes proves every prefix is a bound parameter
// (never inlined) and that the term shape drives the SQL body.
func TestAccountBalanceTermsBindPrefixes(t *testing.T) {
	ab := accountBalance{
		Fact: "rpt_fact_trial_balance_daily",
		Terms: []accountTerm{
			{Side: "debit", Prefixes: []string{"30"}},
			{Side: "credit", Prefixes: []string{"305"}, Sign: sign(-1)},
		},
	}
	parts, args, err := ab.termsExpr([]any{"tenant"})
	if err != nil {
		t.Fatalf("termsExpr: %v", err)
	}
	expr := strings.Join(parts, "")
	if len(args) != 3 {
		t.Fatalf("args = %v, want tenant plus two bound prefixes", args)
	}
	if strings.Contains(expr, "30%") || strings.Contains(expr, "305%") {
		t.Fatalf("prefix was inlined instead of bound: %s", expr)
	}
	if !strings.Contains(expr, "close_debit_minor") || !strings.Contains(expr, "close_credit_minor") {
		t.Fatalf("expr misses a balance side: %s", expr)
	}
	if !strings.Contains(expr, "-") {
		t.Fatalf("subtracted term did not render a minus: %s", expr)
	}
}

// TestAccountBalanceNetAndClamp: a net term computes debit-credit, and a
// positive clamp renders GREATEST(...,0) so "nếu DN > DC" never goes negative.
func TestAccountBalanceNetAndClamp(t *testing.T) {
	ab := accountBalance{
		Fact:  "rpt_fact_trial_balance_daily",
		Terms: []accountTerm{{Side: "net", Prefixes: []string{"5"}, Clamp: "positive"}},
	}
	parts, _, err := ab.termsExpr([]any{"tenant"})
	if err != nil {
		t.Fatalf("termsExpr: %v", err)
	}
	expr := strings.Join(parts, "")
	if !strings.Contains(expr, "GREATEST(") {
		t.Fatalf("positive clamp not rendered: %s", expr)
	}
	if !strings.Contains(expr, "-") {
		t.Fatalf("net term must subtract credit from debit: %s", expr)
	}
}

// TestAccountBalanceEvalValidatesBeforeQuery: a malformed block must fail
// before any SQL is built (the engine is constructed without a repo here).
func TestAccountBalanceEvalValidatesBeforeQuery(t *testing.T) {
	e := &Engine{}
	a := accountBalance{Fact: "rpt_fact_trial_balance_daily", Terms: []accountTerm{{Side: "debit", Prefixes: []string{"10%' --"}}}}
	if _, err := e.evalAccountBalance(tContext(), "tenant-1", "2026-09", &a, nil); err == nil {
		t.Fatal("invalid prefix must fail before querying")
	}
}
