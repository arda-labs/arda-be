package reports

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/xuri/excelize/v2"
)

// ToExcelX renders a report result into XLSX bytes (one sheet, header row +
// data rows). The output bytes are uploaded to media-service by the caller
// (report export flow, Q8).
func ToExcelX(sheetName string, columns []string, rows [][]any) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := "Report"
	if sheetName != "" {
		sheet = sheetName
	}
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, fmt.Errorf("rename sheet: %w", err)
	}

	for i, col := range columns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, col); err != nil {
			return nil, fmt.Errorf("header %s: %w", col, err)
		}
	}
	for r, row := range rows {
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
			if err := f.SetCellValue(sheet, cell, cellValue(v)); err != nil {
				return nil, fmt.Errorf("cell %s: %w", cell, err)
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("write workbook: %w", err)
	}
	return buf.Bytes(), nil
}

// cellValue normalises one raw SQL value for excelize.SetCellValue. A pgx
// scan into a generic destination can surface a PostgreSQL NUMERIC column as
// pgtype.Numeric, whose fmt.Sprint output is a Go struct dump (for example
// "{5 -4 false 0 true}") — excelize's default branch would write that dump
// into the cell. Render it as its exact decimal text instead (Value() keeps
// full precision; Float64Value is only a fallback). Everything else passes
// through unchanged: int64/float64/bool/string/time.Time are already
// supported by excelize.
func cellValue(v any) any {
	switch n := v.(type) {
	case pgtype.Numeric:
		return numericCellText(n)
	case *pgtype.Numeric:
		if n == nil {
			return nil
		}
		return numericCellText(*n)
	default:
		return v
	}
}

// numericCellText returns the decimal text of a valid NUMERIC, or nil for SQL
// NULL so the cell stays empty rather than showing "<nil>".
func numericCellText(n pgtype.Numeric) any {
	if !n.Valid {
		return nil
	}
	if s, err := n.Value(); err == nil {
		return s
	}
	if f, err := n.Float64Value(); err == nil && f.Valid {
		return f.Float64
	}
	return nil
}
