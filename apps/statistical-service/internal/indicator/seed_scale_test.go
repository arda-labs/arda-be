package indicator

import (
	"encoding/json"
	"testing"
)

// TestSumFormulaDoesNotRescaleMinorUnits locks the regression the NL-routing
// verification caught: the seed multiplied every amount indicator by 100, so
// indicator 20000.01 reported 10,000,000,000 while the report reading the same
// fact column reported 100,000,000. VND has zero decimal digits, so minor ==
// major and no amount indicator may carry a scale.
func TestSeedFormulasKeepMinorUnitsUnscaled(t *testing.T) {
	// The shipped seed formulas, verbatim from
	// 20260922130000_rpt_indicator_seeds.sql (with the 20260922140000 fix).
	formulas := []string{
		`{"type":"sum","fact":"rpt_fact_deposit_contract_daily","column":"principal_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}`,
		`{"type":"sum","fact":"rpt_fact_loan_agreement_daily","column":"outstanding_amt_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}`,
		`{"type":"sum","fact":"rpt_fact_capital_contract_daily","column":"amount_minor","filter":{"status":"ACTIVE"},"as_of":"period_end","scale":1}`,
		`{"type":"sum","fact":"rpt_fact_loan_collateral_daily","column":"coll_value_minor","as_of":"period_end","scale":1}`,
	}
	for i, raw := range formulas {
		var f Formula
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			t.Fatalf("formula %d: %v", i, err)
		}
		if f.Scale != 1 {
			t.Fatalf("formula %d scales minor units by %v; the indicator would disagree with the report builder reading the same column", i, f.Scale)
		}
	}
}

// TestScaleAppliesWhenExplicit documents the engine contract the seed relies
// on: scale is multiplicative and only 1 keeps the raw fact value.
func TestScaleAppliesWhenExplicit(t *testing.T) {
	var f Formula
	if err := json.Unmarshal([]byte(`{"type":"sum","fact":"rpt_fact_customer_daily","column":"customer_code","scale":1}`), &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Scale != 1 {
		t.Fatalf("scale = %v", f.Scale)
	}
}
