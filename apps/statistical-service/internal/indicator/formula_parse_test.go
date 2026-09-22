package indicator

import (
	"encoding/json"
	"testing"
)

// TestParseAccountFormulaHandlesCatalogForms locks the grammar against the real
// shapes in the PCF catalog: single sides, totals, subtraction, net accounts,
// and the "if positive" condition.
func TestParseAccountFormulaHandlesCatalogForms(t *testing.T) {
	cases := []struct {
		name    string
		formula string
		want    []accountTerm
	}{
		{
			name:    "simple debit",
			formula: "[DCN TK 10]",
			want:    []accountTerm{{Side: "debit", Prefixes: []string{"10"}}},
		},
		{
			name:    "simple credit",
			formula: "DCC TK 139",
			want:    []accountTerm{{Side: "credit", Prefixes: []string{"139"}}},
		},
		{
			name:    "dư nợ alias",
			formula: "Dư nợ TK 31",
			want:    []accountTerm{{Side: "debit", Prefixes: []string{"31"}}},
		},
		{
			name:    "dư có alias",
			formula: "Dư có TK 3599",
			want:    []accountTerm{{Side: "credit", Prefixes: []string{"3599"}}},
		},
		{
			name:    "fixed assets net",
			formula: "DCN TK 30 - DCC TK 305",
			want: []accountTerm{
				{Side: "debit", Prefixes: []string{"30"}},
				{Side: "credit", Prefixes: []string{"305"}, Sign: sign(-1)},
			},
		},
		{
			name:    "profit debit minus credit",
			formula: "DCC TK 7 - DCN TK 8",
			want: []accountTerm{
				{Side: "credit", Prefixes: []string{"7"}},
				{Side: "debit", Prefixes: []string{"8"}, Sign: sign(-1)},
			},
		},
		{
			name:    "total debit list",
			formula: "Tổng DCN các TK (10, 11, 13)",
			want:    []accountTerm{{Side: "debit", Prefixes: []string{"10", "11", "13"}}},
		},
		{
			name:    "total minus total",
			formula: "Tổng DCN các TK (10, 11) - Tổng DCC các TK (139, 209)",
			want: []accountTerm{
				{Side: "debit", Prefixes: []string{"10", "11"}},
				{Side: "credit", Prefixes: []string{"139", "209"}, Sign: sign(-1)},
			},
		},
		{
			name:    "exclusion removes a prefix",
			formula: "DCN TK 38 (trừ TK 386)",
			want:    []accountTerm{{Side: "debit", Prefixes: []string{"38"}, Exclude: []string{"386"}}},
		},
		{
			name:    "condition clamps every term",
			formula: "( DCN TK 5 - DCC TK 5 ) nếu DCN - DCC>0",
			want: []accountTerm{
				{Side: "debit", Prefixes: []string{"5"}, Clamp: "positive"},
				{Side: "credit", Prefixes: []string{"5"}, Sign: sign(-1), Clamp: "positive"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ab, err := ParseAccountFormula(c.formula)
			if err != nil {
				t.Fatalf("parse %q: %v", c.formula, err)
			}
			if err := ab.validate(); err != nil {
				t.Fatalf("parsed block does not validate: %v", err)
			}
			if len(ab.Terms) != len(c.want) {
				t.Fatalf("terms = %+v, want %+v", ab.Terms, c.want)
			}
			for i := range c.want {
				if ab.Terms[i].Side != c.want[i].Side {
					t.Errorf("term %d side = %q, want %q", i, ab.Terms[i].Side, c.want[i].Side)
				}
				if ab.Terms[i].Clamp != c.want[i].Clamp {
					t.Errorf("term %d clamp = %q, want %q", i, ab.Terms[i].Clamp, c.want[i].Clamp)
				}
				if signOf(ab.Terms[i].Sign) != signOf(c.want[i].Sign) {
					t.Errorf("term %d sign = %d, want %d", i, signOf(ab.Terms[i].Sign), signOf(c.want[i].Sign))
				}
				if got, want := len(ab.Terms[i].Prefixes), len(c.want[i].Prefixes); got != want {
					t.Errorf("term %d prefixes = %v, want %v", i, ab.Terms[i].Prefixes, c.want[i].Prefixes)
				}
				if got, want := len(ab.Terms[i].Exclude), len(c.want[i].Exclude); got != want {
					t.Errorf("term %d exclude = %v, want %v", i, ab.Terms[i].Exclude, c.want[i].Exclude)
				}
			}
		})
	}
}

// TestParseAccountFormulaRejectsProse: descriptive formulas must fail rather
// than produce a guessed number.
func TestParseAccountFormulaRejectsProse(t *testing.T) {
	rejects := []string{
		"Tự tính = Lợi nhuận trước thuế * Thuế suất thuế TNDN = PSN TK 8331",
		"Trùng công thức với tổng tài sản",
		"=0",
		"",
		"PSN TK 4534",
	}
	for _, f := range rejects {
		if _, err := ParseAccountFormula(f); err == nil {
			t.Fatalf("formula %q should have been rejected", f)
		}
	}
}

// TestParsedFormulaRoundTripsThroughJSON proves the parser output is exactly the
// shape the engine unmarshals (Formula.Accounts).
func TestParsedFormulaRoundTripsThroughJSON(t *testing.T) {
	ab, err := ParseAccountFormula("DCN TK 30 - DCC TK 305")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	blob, err := json.Marshal(Formula{Type: "account_balance", Accounts: &ab})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Formula
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Accounts == nil || len(back.Accounts.Terms) != 2 {
		t.Fatalf("round-trip lost the terms: %s", blob)
	}
	if err := back.Accounts.validate(); err != nil {
		t.Fatalf("round-tripped block does not validate: %v", err)
	}
}

func signOf(p *int) int {
	if p == nil {
		return 1
	}
	return *p
}
