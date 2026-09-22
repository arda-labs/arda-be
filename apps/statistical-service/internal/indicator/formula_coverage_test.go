package indicator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// catalogFormula is one row of the PCF accounting catalog extract kept in
// testdata (generated from docs/epas-survey/exports/pcf-kpi-catalog.json).
type catalogFormula struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Formula string `json:"formula"`
}

// TestParseAccountFormulaCatalogCoverage runs the parser over every real
// accounting P formula and asserts a floor on coverage. It prints the rejected
// codes so a shrinking grammar is visible, and keeps the floor high enough that
// a regression in a common term shape fails the build.
//
// The floor is deliberate: the grammar is allowed to reject genuinely
// descriptive formulas (they cannot be derived from account balances), but it
// must keep parsing the account-shaped majority.
func TestParseAccountFormulaCatalogCoverage(t *testing.T) {
	blob, err := os.ReadFile(filepath.Join("testdata", "account_formulas.json"))
	if err != nil {
		t.Skipf("catalog fixture not present: %v", err)
	}
	var rows []catalogFormula
	if err := json.Unmarshal(blob, &rows); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("fixture is empty")
	}

	parsed, rejected := 0, []string{}
	for _, row := range rows {
		ab, err := ParseAccountFormula(row.Formula)
		if err != nil {
			rejected = append(rejected, row.Code+" "+row.Name)
			continue
		}
		// A parsed block must satisfy the engine contract, or the seed would
		// fail at compute time instead of parse time.
		if err := ab.validate(); err != nil {
			t.Errorf("%s parsed but does not validate: %v", row.Code, err)
			continue
		}
		parsed++
	}

	const minCoverage = 100
	if parsed < minCoverage {
		t.Fatalf("parser covered %d/%d accounting formulas, want at least %d\nrejected:\n  %v",
			parsed, len(rows), minCoverage, rejected)
	}
	t.Logf("accounting formula coverage: %d/%d parsed, %d rejected", parsed, len(rows), len(rejected))
	for _, r := range rejected {
		t.Logf("  rejected: %s", r)
	}
}
