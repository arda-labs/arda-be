package reports

import (
	"bytes"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/xuri/excelize/v2"
)

func TestToExcelXShape(t *testing.T) {
	data, err := ToExcelX("Summary",
		[]string{"debt_group", "count"},
		[][]any{{"GROUP_1", int64(3)}, {"GROUP_2", int64(7)}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	rows, err := f.GetRows("Summary")
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2)", len(rows))
	}
	if rows[0][0] != "debt_group" || rows[1][0] != "GROUP_1" || rows[2][1] != "7" {
		t.Fatalf("cell values wrong: %v", rows)
	}
}

func TestCellValueNumeric(t *testing.T) {
	var n pgtype.Numeric
	if err := n.Scan("12.34"); err != nil {
		t.Fatalf("scan numeric: %v", err)
	}
	if got := cellValue(n); got != "12.34" {
		t.Fatalf("cellValue(pgtype.Numeric) = %#v, want %q", got, "12.34")
	}
	if got := cellValue(&n); got != "12.34" {
		t.Fatalf("cellValue(*pgtype.Numeric) = %#v, want %q", got, "12.34")
	}
	if got := cellValue(pgtype.Numeric{}); got != nil {
		t.Fatalf("invalid numeric = %#v, want nil (NULL cell)", got)
	}
	if got := cellValue((*pgtype.Numeric)(nil)); got != nil {
		t.Fatalf("nil numeric pointer = %#v, want nil (NULL cell)", got)
	}
	// Non-numeric values must pass through unchanged.
	if got := cellValue(int64(7)); got != int64(7) {
		t.Fatalf("int64 passthrough = %#v", got)
	}
	if got := cellValue("GROUP_1"); got != "GROUP_1" {
		t.Fatalf("string passthrough = %#v", got)
	}
}

// A pgtype.Numeric must land in the cell as its decimal text, not as a Go
// struct dump ("{5 -4 false 0 true}"), and SQL NULL must stay empty.
func TestToExcelXNumericCell(t *testing.T) {
	var n pgtype.Numeric
	if err := n.Scan("12.34"); err != nil {
		t.Fatalf("scan numeric: %v", err)
	}
	data, err := ToExcelX("Summary",
		[]string{"avg_interest_rate", "note"},
		[][]any{{n, "ok"}, {pgtype.Numeric{}, "null"}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	rows, err := f.GetRows("Summary")
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2)", len(rows))
	}
	if rows[1][0] != "12.34" {
		t.Fatalf("numeric cell = %q, want %q", rows[1][0], "12.34")
	}
	if len(rows[2]) > 0 && rows[2][0] != "" {
		t.Fatalf("NULL numeric cell = %q, want empty", rows[2][0])
	}
}
