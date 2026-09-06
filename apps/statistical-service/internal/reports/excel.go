package reports

import (
	"fmt"

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
			if err := f.SetCellValue(sheet, cell, v); err != nil {
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
