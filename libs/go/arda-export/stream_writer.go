package ardaexport

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// CellType defines the export cell formatting behavior.
type CellType string

const (
	CellTypeString   CellType = "string"
	CellTypeCode     CellType = "code" // Preserves leading zeros ("0012345678")
	CellTypeNumber   CellType = "number"
	CellTypeCurrency CellType = "currency"
	CellTypeDate     CellType = "date"
	CellTypeBoolean  CellType = "boolean"
)

// Column defines metadata and formatting rules for a single export column.
type Column struct {
	Header    string
	Key       string
	Type      CellType
	Width     float64
	Formatter func(val any) any
}

// StreamOptions contains configuration for report generation.
type StreamOptions struct {
	Title       string
	SheetName   string
	Columns     []Column
	Metadata    map[string]string
	Locale      string // "vi-VN" (default) or "en-US"
	Timezone    *time.Location
	ScopeLabel  string
	TotalCount  int
}

// RowIterator abstracts database rows or memory iterators.
type RowIterator interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// RowSupplier returns a single row's raw values on each call. Returns (nil, io.EOF) when done.
type RowSupplier func() ([]any, error)

var leadingZeroRegex = regexp.MustCompile(`^0\d+$`)

// StreamXLSX writes rows directly to io.Writer using excelize.StreamWriter for O(1) memory usage.
func StreamXLSX(ctx context.Context, w io.Writer, opts StreamOptions, supplier RowSupplier) error {
	f := excelize.NewFile()
	defer f.Close()

	sheet := opts.SheetName
	if sheet == "" {
		sheet = "Sheet1"
	}
	if sheet != "Sheet1" {
		f.NewSheet(sheet)
		f.DeleteSheet("Sheet1")
	}

	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return fmt.Errorf("create stream writer: %w", err)
	}

	loc := opts.Timezone
	if loc == nil {
		loc = time.Local
	}
	isEn := opts.Locale == "en-US"

	// 1. Title Banner (Row 1)
	title := strings.ToUpper(strings.TrimSpace(opts.Title))
	if title == "" {
		if isEn {
			title = "DATA EXPORT REPORT"
		} else {
			title = "BÁO CÁO TRÍCH XUẤT DỮ LIỆU"
		}
	}
	if err := sw.SetRow("A1", []any{title}); err != nil {
		return err
	}

	// 2. Audit Metadata Line (Row 2)
	nowStr := time.Now().In(loc).Format("02/01/2006 15:04:05")
	var metadataParts []string
	if isEn {
		metadataParts = append(metadataParts, fmt.Sprintf("Exported at: %s", nowStr))
		if opts.ScopeLabel != "" {
			metadataParts = append(metadataParts, fmt.Sprintf("Scope: %s", opts.ScopeLabel))
		}
		if opts.TotalCount > 0 {
			metadataParts = append(metadataParts, fmt.Sprintf("Total records: %d", opts.TotalCount))
		}
	} else {
		metadataParts = append(metadataParts, fmt.Sprintf("Thời gian trích xuất: %s", nowStr))
		if opts.ScopeLabel != "" {
			metadataParts = append(metadataParts, fmt.Sprintf("Phạm vi: %s", opts.ScopeLabel))
		}
		if opts.TotalCount > 0 {
			metadataParts = append(metadataParts, fmt.Sprintf("Tổng số bản ghi: %d", opts.TotalCount))
		}
	}
	for k, v := range opts.Metadata {
		metadataParts = append(metadataParts, fmt.Sprintf("%s: %s", k, v))
	}
	if err := sw.SetRow("A2", []any{strings.Join(metadataParts, "  |  ")}); err != nil {
		return err
	}

	// 3. Blank separator (Row 3)
	if err := sw.SetRow("A3", []any{}); err != nil {
		return err
	}

	// 4. Column Headers (Row 4)
	headers := make([]any, len(opts.Columns))
	for i, c := range opts.Columns {
		headers[i] = c.Header
	}
	if err := sw.SetRow("A4", headers); err != nil {
		return err
	}

	// 5. Data Rows (Row 5+)
	rowIdx := 5
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rawRow, err := supplier()
		if err == io.EOF && len(rawRow) == 0 {
			break
		}
		if err != nil && err != io.EOF {
			return fmt.Errorf("read row data: %w", err)
		}
		if rawRow == nil {
			break
		}

		cells := make([]any, len(opts.Columns))
		for cIdx, col := range opts.Columns {
			var rawVal any
			if cIdx < len(rawRow) {
				rawVal = rawRow[cIdx]
			}
			if col.Formatter != nil {
				rawVal = col.Formatter(rawVal)
			}
			cells[cIdx] = formatExcelCell(rawVal, col.Type, isEn, loc)
		}

		cellRef, _ := excelize.CoordinatesToCellName(1, rowIdx)
		if err := sw.SetRow(cellRef, cells); err != nil {
			return fmt.Errorf("write stream row %d: %w", rowIdx, err)
		}
		rowIdx++
	}

	if err := sw.Flush(); err != nil {
		return fmt.Errorf("flush stream: %w", err)
	}

	// Set Freeze Panes at Row 4 so header is locked on scrolling
	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      4,
		TopLeftCell: "A5",
		ActivePane:  "bottomLeft",
	})

	// Set default column widths
	for i, col := range opts.Columns {
		colName, _ := excelize.ColumnNumberToName(i + 1)
		w := col.Width
		if w <= 0 {
			w = float64(maxInt(len(col.Header)+4, 15))
		}
		_ = f.SetColWidth(sheet, colName, colName, w)
	}

	return f.Write(w)
}

// StreamCSV writes rows to io.Writer in UTF-8 CSV with BOM and RFC 4180 escaping.
func StreamCSV(ctx context.Context, w io.Writer, opts StreamOptions, supplier RowSupplier) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	// Write UTF-8 BOM for Microsoft Excel compatibility
	if _, err := bw.WriteString("\uFEFF"); err != nil {
		return err
	}

	cw := csv.NewWriter(bw)
	defer cw.Flush()

	isEn := opts.Locale == "en-US"
	loc := opts.Timezone
	if loc == nil {
		loc = time.Local
	}

	// Header row
	headerRecord := make([]string, len(opts.Columns))
	for i, col := range opts.Columns {
		headerRecord[i] = col.Header
	}
	if err := cw.Write(headerRecord); err != nil {
		return err
	}

	// Data rows
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rawRow, err := supplier()
		if err == io.EOF && len(rawRow) == 0 {
			break
		}
		if err != nil && err != io.EOF {
			return err
		}
		if rawRow == nil {
			break
		}

		record := make([]string, len(opts.Columns))
		for cIdx, col := range opts.Columns {
			var rawVal any
			if cIdx < len(rawRow) {
				rawVal = rawRow[cIdx]
			}
			if col.Formatter != nil {
				rawVal = col.Formatter(rawVal)
			}
			record[cIdx] = formatCSVCell(rawVal, col.Type, isEn, loc)
		}

		if err := cw.Write(record); err != nil {
			return err
		}
	}

	return nil
}

func formatExcelCell(v any, colType CellType, isEn bool, loc *time.Location) any {
	if v == nil {
		return ""
	}

	switch colType {
	case CellTypeCode:
		return fmt.Sprintf("%v", v)
	case CellTypeBoolean:
		if b, ok := v.(bool); ok {
			if b {
				if isEn {
					return "Yes"
				}
				return "Có"
			}
			if isEn {
				return "No"
			}
			return "Không"
		}
	case CellTypeDate:
		if t, ok := v.(time.Time); ok {
			if t.IsZero() {
				return "-"
			}
			return t.In(loc).Format("02/01/2006 15:04:05")
		}
	case CellTypeNumber, CellTypeCurrency:
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return n
		case int32:
			return n
		case float64:
			return n
		case float32:
			return float64(n)
		case string:
			if num, err := strconv.ParseFloat(n, 64); err == nil {
				return num
			}
		}
	}

	// Fallback type checks
	switch val := v.(type) {
	case string:
		// Preserve leading zero strings as text explicitly
		if leadingZeroRegex.MatchString(val) {
			return val
		}
		return val
	case time.Time:
		if val.IsZero() {
			return "-"
		}
		return val.In(loc).Format("02/01/2006 15:04:05")
	case []string:
		return strings.Join(val, ", ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatCSVCell(v any, colType CellType, isEn bool, loc *time.Location) string {
	if v == nil {
		return ""
	}
	switch colType {
	case CellTypeBoolean:
		if b, ok := v.(bool); ok {
			if b {
				if isEn {
					return "Yes"
				}
				return "Có"
			}
			if isEn {
				return "No"
			}
			return "Không"
		}
	case CellTypeDate:
		if t, ok := v.(time.Time); ok {
			if t.IsZero() {
				return "-"
			}
			return t.In(loc).Format("02/01/2006 15:04:05")
		}
	}

	switch val := v.(type) {
	case time.Time:
		if val.IsZero() {
			return "-"
		}
		return val.In(loc).Format("02/01/2006 15:04:05")
	case []string:
		return strings.Join(val, ", ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
