package reports

import (
	"bytes"
	"testing"

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
