package indicator

import "testing"

// TestDirectAccountSumRefusesClampedTerms: the reconciliation's direct path
// deliberately reimplements the arithmetic, so it refuses any block it cannot
// mirror (a clamp) rather than comparing two different formulas.
func TestDirectAccountSumRefusesClampedTerms(t *testing.T) {
	ab := accountBalance{
		Fact:  "rpt_fact_trial_balance_daily",
		Terms: []accountTerm{{Side: "net", Prefixes: []string{"5"}, Clamp: "positive"}},
	}
	// nil repo: the guard must fire before any query is attempted.
	if _, err := directAccountSum(tContext(), nil, "tenant-1", "2026-09", &ab); err == nil {
		t.Fatal("a clamped block must be refused by the direct reconciliation path")
	}
}

// TestAlmostEqualUsesMinorUnitTolerance keeps the comparison honest for
// integer minor amounts surfaced as float64.
func TestAlmostEqualUsesMinorUnitTolerance(t *testing.T) {
	if !almostEqual(100, 100) {
		t.Fatal("identical values must match")
	}
	if !almostEqual(100, 100.4) {
		t.Fatal("sub-unit float noise must match")
	}
	if almostEqual(100, 101) {
		t.Fatal("a whole minor unit difference must not match")
	}
}
