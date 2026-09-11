package ardadoc

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func buildTestTemplate(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetCellValue("Sheet1", "A1", "Header"); err != nil {
		t.Fatalf("set A1: %v", err)
	}
	if err := f.SetDefinedName(&excelize.DefinedName{Name: "Total", RefersTo: "Sheet1!$B$2"}); err != nil {
		t.Fatalf("set defined name Total: %v", err)
	}
	if err := f.SetDefinedName(&excelize.DefinedName{Name: "Rows", RefersTo: "Sheet1!$A$4:$B$4"}); err != nil {
		t.Fatalf("set defined name Rows: %v", err)
	}
	if err := f.SetDefinedName(&excelize.DefinedName{Name: "HeaderCell", RefersTo: "Sheet1!$A$1:$B$1"}); err != nil {
		t.Fatalf("set defined name HeaderCell: %v", err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("write template: %v", err)
	}
	return buf.Bytes()
}

func TestFillXLSXTemplate(t *testing.T) {
	template := buildTestTemplate(t)

	out, err := FillXLSXTemplate(template,
		map[string]any{
			"Total":       1234.5,
			"Sheet1!$C$1": "direct-ref",
			"HeaderCell":  "Filled",
		},
		map[string][][]any{
			"Rows": {{"alpha", 1}, {"beta", 2}},
		},
	)
	if err != nil {
		t.Fatalf("FillXLSXTemplate: %v", err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()

	cases := map[string]string{
		"B2": "1234.5", // defined name → single cell
		"C1": "direct-ref",
		"A1": "Filled", // range defined name → top-left anchor
		"A4": "alpha",  // table anchor row 1
		"B4": "1",
		"A5": "beta", // table advances rows
		"B5": "2",
	}
	for cell, want := range cases {
		got, err := f.GetCellValue("Sheet1", cell)
		if err != nil {
			t.Fatalf("get cell %s: %v", cell, err)
		}
		if got != want {
			t.Errorf("cell %s = %q, want %q", cell, got, want)
		}
	}
}

func TestFillXLSXTemplateUnknownName(t *testing.T) {
	if _, err := FillXLSXTemplate(buildTestTemplate(t), map[string]any{"Nope": 1}, nil); err == nil {
		t.Fatal("expected error for unknown name")
	}
}

func TestSplitSheetCell(t *testing.T) {
	cases := []struct {
		ref       string
		sheet     string
		cell      string
		wantError bool
	}{
		{ref: "Sheet1!$B$2", sheet: "Sheet1", cell: "B2"},
		{ref: "'My Sheet'!a1", sheet: "My Sheet", cell: "A1"},
		{ref: "Sheet1!$A$1:$B$9", sheet: "Sheet1", cell: "A1"},
		{ref: "no-separator", wantError: true},
		{ref: "Sheet1!", wantError: true},
	}
	for _, tc := range cases {
		sheet, cell, err := splitSheetCell(tc.ref)
		if tc.wantError {
			if err == nil {
				t.Errorf("splitSheetCell(%q) = %q,%q; want error", tc.ref, sheet, cell)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitSheetCell(%q): %v", tc.ref, err)
			continue
		}
		if sheet != tc.sheet || cell != tc.cell {
			t.Errorf("splitSheetCell(%q) = %q,%q; want %q,%q", tc.ref, sheet, cell, tc.sheet, tc.cell)
		}
	}
}
