package service

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// ToExcel renders statement rows into XLSX bytes (EPAS-style fixed sheet,
// label indent by level, signed amounts, bold totals). Delivered as a sync
// blob — statement sizes are tens of rows; async job handoff (EPAS
// RPT_EXPORT_JOB) stays out of scope until exports get heavy.
func ToExcel(res *StatementResult) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Statement"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, fmt.Errorf("rename sheet: %w", err)
	}

	title := res.StatementCode
	if res.AsOf != "" {
		title += " — " + res.AsOf
	}
	if err := f.SetCellValue(sheet, "A1", title); err != nil {
		return nil, fmt.Errorf("title: %w", err)
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("style: %w", err)
	}
	if err := f.SetCellStyle(sheet, "A1", "A1", bold); err != nil {
		return nil, fmt.Errorf("title style: %w", err)
	}
	if err := f.SetCellValue(sheet, "A2", "Row"); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	if err := f.SetCellValue(sheet, "B2", "Label"); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	if err := f.SetCellValue(sheet, "C2", "Amount"); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	if err := f.SetCellStyle(sheet, "A2", "C2", bold); err != nil {
		return nil, fmt.Errorf("header style: %w", err)
	}

	for i, r := range res.Rows {
		line := i + 3
		cell, _ := excelize.CoordinatesToCellName(1, line)
		if err := f.SetCellValue(sheet, cell, r.RowCode); err != nil {
			return nil, fmt.Errorf("row %s: %w", r.RowCode, err)
		}
		cell, _ = excelize.CoordinatesToCellName(2, line)
		if err := f.SetCellValue(sheet, cell, indent(r.Level)+r.Label); err != nil {
			return nil, fmt.Errorf("row %s: %w", r.RowCode, err)
		}
		cell, _ = excelize.CoordinatesToCellName(3, line)
		if r.HasAmount {
			if err := f.SetCellValue(sheet, cell, r.AmountMinor); err != nil {
				return nil, fmt.Errorf("row %s: %w", r.RowCode, err)
			}
		}
		if r.IsTotal {
			if err := f.SetCellStyle(sheet, cell, cell, bold); err != nil {
				return nil, fmt.Errorf("row %s style: %w", r.RowCode, err)
			}
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("write workbook: %w", err)
	}
	return buf.Bytes(), nil
}

func indent(level int) string {
	out := ""
	for range level {
		out += "    "
	}
	return out
}
