package ardadoc

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// FillXLSXTemplate fills an .xlsx template in place and returns the rendered
// workbook bytes.
//
// Both values and tables keys resolve to a target cell either by workbook
// defined name or by an explicit "SheetName!B5" reference (dollar signs are
// ignored). Values write a single cell; tables write a rectangular block of
// rows whose top-left corner is the resolved anchor cell, advancing one row
// and one column per element. Existing cell styles are preserved for the
// anchor cell but not replicated across the written block, so templates that
// need per-row styling should pre-style the anchor range or keep it plain.
func FillXLSXTemplate(template []byte, values map[string]any, tables map[string][][]any) ([]byte, error) {
	f, err := excelize.OpenReader(bytes.NewReader(template))
	if err != nil {
		return nil, fmt.Errorf("open xlsx template: %w", err)
	}
	defer f.Close()

	resolver, err := newNameResolver(f)
	if err != nil {
		return nil, err
	}

	for name, value := range values {
		sheet, cell, err := resolver.resolve(name)
		if err != nil {
			return nil, fmt.Errorf("resolve value %q: %w", name, err)
		}
		if err := f.SetCellValue(sheet, cell, value); err != nil {
			return nil, fmt.Errorf("set value %q: %w", name, err)
		}
	}

	for name, rows := range tables {
		sheet, anchor, err := resolver.resolve(name)
		if err != nil {
			return nil, fmt.Errorf("resolve table %q: %w", name, err)
		}
		if err := writeTableRows(f, sheet, anchor, rows); err != nil {
			return nil, fmt.Errorf("write table %q: %w", name, err)
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}
	return buf.Bytes(), nil
}

func writeTableRows(f *excelize.File, sheet, anchor string, rows [][]any) error {
	anchorCol, anchorRow, err := excelize.CellNameToCoordinates(anchor)
	if err != nil {
		return fmt.Errorf("parse anchor %q: %w", anchor, err)
	}
	for rowOffset, row := range rows {
		for colOffset, value := range row {
			cell, err := excelize.CoordinatesToCellName(anchorCol+colOffset, anchorRow+rowOffset)
			if err != nil {
				return fmt.Errorf("compute cell for row %d col %d: %w", rowOffset, colOffset, err)
			}
			if err := f.SetCellValue(sheet, cell, value); err != nil {
				return fmt.Errorf("set cell %s: %w", cell, err)
			}
		}
	}
	return nil
}

// nameResolver maps keys to (sheet, cell) targets, supporting workbook
// defined names and explicit "Sheet!Cell" references.
type nameResolver struct {
	definedNames map[string]string // name → RefersTo
}

func newNameResolver(f *excelize.File) (*nameResolver, error) {
	defined := make(map[string]string)
	for _, dn := range f.GetDefinedName() {
		if dn.Name != "" && dn.RefersTo != "" {
			defined[dn.Name] = dn.RefersTo
		}
	}
	return &nameResolver{definedNames: defined}, nil
}

func (r *nameResolver) resolve(key string) (sheet, cell string, err error) {
	if ref, ok := r.definedNames[key]; ok {
		return splitSheetCell(ref)
	}
	if strings.Contains(key, "!") {
		return splitSheetCell(key)
	}
	return "", "", fmt.Errorf("unknown template name (no defined name and no ! separator): %s", key)
}

// splitSheetCell parses "Sheet1!$A$1" (or "'My Sheet'!$A$1") into sheet and
// cell name.
func splitSheetCell(ref string) (sheet, cell string, err error) {
	idx := strings.LastIndex(ref, "!")
	if idx <= 0 || idx == len(ref)-1 {
		return "", "", fmt.Errorf("malformed sheet!cell reference: %s", ref)
	}
	sheet = strings.Trim(strings.TrimSpace(ref[:idx]), "'")
	cell = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(ref[idx+1:]), "$", ""))
	if !strings.Contains(cell, ":") {
		return sheet, cell, nil
	}
	// Range defined name: use the top-left corner as the anchor.
	parts := strings.SplitN(cell, ":", 2)
	return sheet, parts[0], nil
}
