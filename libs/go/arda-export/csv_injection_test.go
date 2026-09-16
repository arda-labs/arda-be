package ardaexport

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// TestFormatCSVCell_FormulaInjection covers the CSV formula injection guard
// (CWE-1236): untrusted strings must not start with a character that
// spreadsheet applications treat as a formula, while numeric/boolean/time
// values keep their existing formatting.
func TestFormatCSVCell_FormulaInjection(t *testing.T) {
	t.Parallel()

	loc := time.UTC
	tests := []struct {
		name    string
		value   any
		colType CellType
		want    string
	}{
		{
			name:    "hyperlink formula",
			value:   `=HYPERLINK("http://evil","x")`,
			colType: CellTypeString,
			want:    `'=HYPERLINK("http://evil","x")`,
		},
		{name: "plus formula", value: "+1+1", colType: CellTypeString, want: "'+1+1"},
		{name: "minus formula", value: "-1+1", colType: CellTypeString, want: "'-1+1"},
		{name: "at formula", value: "@SUM(A1)", colType: CellTypeString, want: "'@SUM(A1)"},
		{name: "tab prefix", value: "\tX", colType: CellTypeString, want: "'\tX"},
		{name: "cr prefix", value: "\rX", colType: CellTypeString, want: "'\rX"},
		{name: "plain text untouched", value: "Nguyễn Văn A", colType: CellTypeString, want: "Nguyễn Văn A"},
		{name: "leading zero code untouched", value: "0012345678", colType: CellTypeCode, want: "0012345678"},
		// A string is still a string even in a numeric column, so it is guarded.
		{name: "numeric-looking string is guarded", value: "-5", colType: CellTypeNumber, want: "'-5"},
		// Real numbers keep their existing formatting.
		{name: "negative int untouched", value: -5, colType: CellTypeNumber, want: "-5"},
		{name: "negative float untouched", value: -5.25, colType: CellTypeCurrency, want: "-5.25"},
		{name: "positive int untouched", value: 42, colType: CellTypeNumber, want: "42"},
		{name: "bool untouched", value: true, colType: CellTypeBoolean, want: "Có"},
		{
			name:    "date untouched",
			value:   time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
			colType: CellTypeDate,
			want:    "15/01/2026 10:30:00",
		},
		{name: "nil untouched", value: nil, colType: CellTypeString, want: ""},
		{name: "empty string untouched", value: "", colType: CellTypeString, want: ""},
		{name: "string slice guarded", value: []string{"=evil", "safe"}, colType: CellTypeString, want: "'=evil, safe"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatCSVCell(tc.value, tc.colType, false, loc); got != tc.want {
				t.Errorf("formatCSVCell(%v, %q) = %q, want %q", tc.value, tc.colType, got, tc.want)
			}
		})
	}

	// Every dangerous prefix must be neutralized for strings.
	for _, prefix := range []string{"=", "+", "-", "@", "\t", "\r"} {
		got := formatCSVCell(prefix+"payload", CellTypeString, false, loc)
		if !strings.HasPrefix(got, "'"+prefix) {
			t.Errorf("string starting with %q was not neutralized: %q", prefix, got)
		}
	}
}

// TestStreamCSV_FormulaInjectionNeutralized verifies the guard on the actual
// CSV output path, including headers.
func TestStreamCSV_FormulaInjectionNeutralized(t *testing.T) {
	t.Parallel()

	cols := []Column{
		{Header: "=HEADER", Key: "h", Type: CellTypeString},
		{Header: "Name", Key: "name", Type: CellTypeString},
		{Header: "Amount", Key: "amount", Type: CellTypeNumber},
	}

	rows := [][]any{
		{`=HYPERLINK("http://evil","x")`, "+1+1", -5},
		{"-1+1", "@SUM(A1)", 10},
		{"\tX", "\rX", 0},
	}

	idx := 0
	supplier := func() ([]any, error) {
		if idx >= len(rows) {
			return nil, io.EOF
		}
		r := rows[idx]
		idx++
		return r, nil
	}

	var buf bytes.Buffer
	if err := StreamCSV(context.Background(), &buf, StreamOptions{Columns: cols, Locale: "vi-VN"}, supplier); err != nil {
		t.Fatalf("StreamCSV returned error: %v", err)
	}

	content := strings.TrimPrefix(buf.String(), "\uFEFF")
	records, err := csv.NewReader(strings.NewReader(content)).ReadAll()
	if err != nil {
		t.Fatalf("parse generated CSV: %v", err)
	}

	want := [][]string{
		{"'=HEADER", "Name", "Amount"},
		{`'=HYPERLINK("http://evil","x")`, "'+1+1", "-5"},
		{"'-1+1", "'@SUM(A1)", "10"},
		{"'\tX", "'\rX", "0"},
	}

	if len(records) != len(want) {
		t.Fatalf("got %d CSV records, want %d: %q", len(records), len(want), records)
	}
	for i := range want {
		for j := range want[i] {
			if records[i][j] != want[i][j] {
				t.Errorf("record %d field %d = %q, want %q", i, j, records[i][j], want[i][j])
			}
		}
	}

	// Defense in depth: every field that originated from a string column must
	// not start with a formula trigger. Column index 2 is numeric and is
	// intentionally exempt (a real negative number must stay "-5").
	for i, rec := range records {
		for j, field := range rec {
			if i > 0 && j == 2 {
				continue
			}
			if field == "" {
				continue
			}
			if strings.ContainsRune("=+-@\t\r", rune(field[0])) {
				t.Errorf("field %q still starts with a dangerous character", field)
			}
		}
	}
}

// TestStreamXLSX_FormulaStringsStoredAsText documents that the XLSX path does
// not create formulas: excelize StreamWriter stores plain strings as inline
// strings, so "=..." is displayed literally without neutralization.
func TestStreamXLSX_FormulaStringsStoredAsText(t *testing.T) {
	t.Parallel()

	cols := []Column{{Header: "Value", Key: "v", Type: CellTypeString}}
	payload := `=HYPERLINK("http://evil","x")`

	called := false
	supplier := func() ([]any, error) {
		if called {
			return nil, io.EOF
		}
		called = true
		return []any{payload}, nil
	}

	var buf bytes.Buffer
	if err := StreamXLSX(context.Background(), &buf, StreamOptions{SheetName: "Data", Columns: cols}, supplier); err != nil {
		t.Fatalf("StreamXLSX returned error: %v", err)
	}

	f, err := excelize.OpenReader(&buf)
	if err != nil {
		t.Fatalf("parse generated XLSX: %v", err)
	}
	defer f.Close()

	// The payload must be readable as literal text and must not have become a
	// formula (excelize writes it as an inline string).
	val, err := f.GetCellValue("Data", "A5")
	if err != nil {
		t.Fatalf("GetCellValue: %v", err)
	}
	if val != payload {
		t.Errorf("XLSX cell value = %q, want literal %q", val, payload)
	}
	formula, err := f.GetCellFormula("Data", "A5")
	if err != nil {
		t.Fatalf("GetCellFormula: %v", err)
	}
	if formula != "" {
		t.Errorf("XLSX cell holds a formula %q; CSV-style neutralization may be required", formula)
	}
}
