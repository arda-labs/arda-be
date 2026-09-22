package indicator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func tContext() context.Context { return context.Background() }

// TestLeafRejectsUnknownFactAndColumn proves the engine fails closed on a
// formula that names a table/column outside the whitelist — the whole point of
// replacing EPAS SQL-as-config.
func TestLeafRejectsUnknownFactAndColumn(t *testing.T) {
	e := &Engine{} // no repo: validation must fail before any query

	cases := []Formula{
		{Type: "sum", Fact: "pg_shadow", Column: "passwd"},
		{Type: "sum", Fact: "rpt_fact_loan_agreement_daily", Column: "tenant_id"},
		{Type: "sum", Fact: "rpt_fact_loan_agreement_daily"}, // no column
		{Type: "count_distinct", Fact: "rpt_fact_customer_daily", Column: "name"},
		{Type: "sum", Fact: "rpt_fact_customer_daily", Column: "customer_code", Filter: map[string]json.RawMessage{
			"status": json.RawMessage(`"ACTIVE"`), "evil": json.RawMessage(`"' OR 1=1"`),
		}},
	}
	for i, f := range cases {
		if _, err := e.leaf(tContext(), "tenant-1", "2026-09", &f, nil); err == nil {
			t.Fatalf("case %d: expected failure for %+v", i, f)
		}
	}
}

// TestLeafRejectsUnknownDimension keeps the dimension surface closed too.
func TestLeafRejectsUnknownDimension(t *testing.T) {
	e := &Engine{}
	f := Formula{Type: "count", Fact: "rpt_fact_customer_daily"}
	if _, err := e.leaf(tContext(), "tenant-1", "2026-09", &f, map[string]string{"nope": "x"}); err == nil {
		t.Fatal("unknown dimension must fail")
	}
	// A dimension that the fact does not expose must also fail: coll_type is
	// only on the collateral fact, not on customers.
	if _, err := e.leaf(tContext(), "tenant-1", "2026-09", &f, map[string]string{"coll_type": "NHA"}); err == nil {
		t.Fatal("dimension not available on the fact must fail")
	}
}

// TestRatioGuardsZeroDenominator: a ratio with no base reports 0, never NaN.
func TestRatioGuardsZeroDenominator(t *testing.T) {
	if got := ratioValue(5, 0, true); got != 0 {
		t.Fatalf("ratioValue(5,0,true) = %v, want 0", got)
	}
	if got := ratioValue(1, 4, true); got != 25 {
		t.Fatalf("ratioValue(1,4,true) = %v, want 25", got)
	}
	if got := ratioValue(1, 4, false); got != 0.25 {
		t.Fatalf("ratioValue(1,4,false) = %v, want 0.25", got)
	}
}

// TestDimensionKeyIsDeterministic preserves the storage key contract.
func TestDimensionKeyIsDeterministic(t *testing.T) {
	a := DimensionKey(map[string]string{"product": "DPM12", "org": "01"})
	b := DimensionKey(map[string]string{"org": "01", "product": "DPM12"})
	if a != b {
		t.Fatalf("dimension key not deterministic: %q vs %q", a, b)
	}
	if a != "org=01|product=DPM12" {
		t.Fatalf("dimension key = %q", a)
	}
	if DimensionKey(nil) != "" {
		t.Fatal("empty dims must yield the total key")
	}
}

// TestEvalSeriesGrowthAndAverage locks the series math (no repository needed).
func TestEvalSeriesGrowthAndAverage(t *testing.T) {
	e := &Engine{}
	growth := Formula{Type: "growth", Indicator: "60000.01", Compare: "previous_period", Percent: true}
	series := map[string]float64{"2026-08": 100, "2026-09": 120}

	v, err := e.evalSeries(&growth, "2026-09", series)
	if err != nil {
		t.Fatalf("growth: %v", err)
	}
	if v != 20 {
		t.Fatalf("growth = %v, want 20%%", v)
	}

	// Missing base ⇒ 0, never a fake spike.
	if v, err := e.evalSeries(&growth, "2026-09", map[string]float64{"2026-09": 120}); err != nil || v != 0 {
		t.Fatalf("growth without base = (%v, %v), want (0, nil)", v, err)
	}

	// Same period last year.
	yoy := Formula{Type: "growth", Indicator: "60000.01", Compare: "same_period_last_year", Percent: true}
	if v, err := e.evalSeries(&yoy, "2026-09", map[string]float64{"2025-09": 80, "2026-09": 120}); err != nil || v != 50 {
		t.Fatalf("yoy = (%v, %v), want (50, nil)", v, err)
	}

	avg := Formula{Type: "trailing_average", Indicator: "20000.01"}
	if v, err := e.evalSeries(&avg, "2026-09", map[string]float64{"2026-07": 90, "2026-08": 100, "2026-09": 110}); err != nil || v != 100 {
		t.Fatalf("trailing average = (%v, %v), want (100, nil)", v, err)
	}
	if v, err := e.evalSeries(&avg, "2026-09", map[string]float64{}); err != nil || v != 0 {
		t.Fatalf("empty average = (%v, %v), want (0, nil)", v, err)
	}
}

// TestTrailingPeriods covers the period window helper.
func TestTrailingPeriods(t *testing.T) {
	ps, err := trailingPeriods("2026-09", 3)
	if err != nil {
		t.Fatalf("trailingPeriods: %v", err)
	}
	want := []string{"2026-07", "2026-08", "2026-09"}
	if len(ps) != len(want) {
		t.Fatalf("got %v, want %v", ps, want)
	}
	for i := range want {
		if ps[i] != want[i] {
			t.Fatalf("got %v, want %v", ps, want)
		}
	}
	// Year boundary.
	ps, _ = trailingPeriods("2026-01", 2)
	if ps[0] != "2025-12" || ps[1] != "2026-01" {
		t.Fatalf("year boundary window wrong: %v", ps)
	}
	if _, err := trailingPeriods("2026-9", 3); err == nil {
		t.Fatal("non-canonical period must fail")
	}
	if p, ok := previousPeriod("2026-01"); !ok || p != "2025-12" {
		t.Fatalf("previousPeriod = (%q, %v)", p, ok)
	}
	if p, err := periodYearsAgo("2026-09", 1); err != nil || p != "2025-09" {
		t.Fatalf("periodYearsAgo = (%q, %v)", p, err)
	}
}

// TestComputeSeriesFallsBackForPlainFormula: a leaf formula through
// ComputeSeries must behave like Compute (series context unused).
func TestComputeSeriesRejectsMissingResolver(t *testing.T) {
	e := &Engine{}
	// growth through eval() must point the caller at ComputeSeries rather than
	// silently returning a wrong value.
	f := Formula{Type: "growth", Indicator: "x"}
	if _, err := e.eval(tContext(), "tenant-1", "2026-09", &f, nil, 0); err == nil {
		t.Fatal("growth through eval must fail and direct the caller to ComputeSeries")
	}
}

func TestDimensionNamesSorted(t *testing.T) {
	if len(DimensionNames) == 0 {
		t.Fatal("no dimensions registered")
	}
	for i := 1; i < len(DimensionNames); i++ {
		if strings.Compare(DimensionNames[i-1], DimensionNames[i]) >= 0 {
			t.Fatalf("dimension names not sorted: %v", DimensionNames)
		}
	}
}

// TestComparisonFilterIsRecognisedAndClosed locks the operator filter the
// membership indicators need: a valid op is accepted, an arbitrary string is
// rejected before any SQL is built.
func TestComparisonFilterIsRecognisedAndClosed(t *testing.T) {
	op, values, ok := comparisonFilter(json.RawMessage(`{"op":">","value":"0"}`))
	if !ok || op != ">" || len(values) != 1 || values[0] != "0" {
		t.Fatalf("comparison filter not recognised: %q %v %v", op, values, ok)
	}
	if !validFilterOp(">=") || validFilterOp("DROP TABLE") {
		t.Fatal("operator whitelist is not closed")
	}
	if _, _, ok := comparisonFilter(json.RawMessage(`"ACTIVE"`)); ok {
		t.Fatal("bare value must not parse as a comparison")
	}
}

// TestLeafRejectsUnknownOperator keeps injection out of the comparison path.
func TestLeafRejectsUnknownOperator(t *testing.T) {
	e := &Engine{}
	f := Formula{
		Type: "count",
		Fact: "rpt_fact_member_daily",
		Filter: map[string]json.RawMessage{
			"total_capital_minor": json.RawMessage(`{"op":"; DROP TABLE x --","value":"0"}`),
		},
	}
	if _, err := e.leaf(tContext(), "tenant-1", "2026-09", &f, nil); err == nil {
		t.Fatal("unknown operator must fail closed")
	}
}
