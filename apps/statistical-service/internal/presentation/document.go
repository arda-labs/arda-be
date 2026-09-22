package presentation

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ReportDocument is everything a printable report needs. It is built by the
// caller from a report result (or an indicator set) and handed to the
// renderers; the templates own all markup.
type ReportDocument struct {
	Title       string
	Subtitle    string
	Period      string
	Org         string
	Columns     []string
	Rows        [][]any
	KPI         []KPI
	Chart       *Chart
	Narrative   string
	GeneratedAt string
}

// KPI is one headline figure shown above the table.
type KPI struct {
	Code  string
	Label string
	Value string
	Unit  string
}

// RenderedFormat is the output a caller asked for.
type RenderedFormat string

const (
	FormatHTML RenderedFormat = "html"
	FormatXLSX RenderedFormat = "xlsx"
)

// HTMLReportTemplate is the print layout consumed by Gotenberg Chromium. It is
// self-contained (style + print rules) because no wrapper CSS is injected, and
// carries Vietnamese-safe font fallbacks.
const HTMLReportTemplate = `<!DOCTYPE html>
<html lang="vi">
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
<style>
  @page { size: A4; margin: 18mm 14mm; }
  body { font-family: "Noto Sans", "DejaVu Sans", Arial, sans-serif; font-size: 11px; color: #1a1a1a; }
  h1 { font-size: 16px; margin: 0 0 2px 0; }
  .meta { color: #555; margin-bottom: 10px; }
  .kpi { display: flex; flex-wrap: wrap; gap: 8px; margin: 10px 0 14px 0; }
  .kpi .card { border: 1px solid #d8d8d8; border-radius: 4px; padding: 6px 10px; min-width: 130px; }
  .kpi .label { color: #666; font-size: 10px; }
  .kpi .value { font-size: 14px; font-weight: 700; }
  table { border-collapse: collapse; width: 100%; margin-top: 8px; }
  th, td { border: 1px solid #d8d8d8; padding: 4px 6px; text-align: left; }
  th { background: #f2f4f8; }
  td.num { text-align: right; font-variant-numeric: tabular-nums; }
  .narrative { margin-top: 12px; white-space: pre-wrap; }
  footer { margin-top: 16px; color: #777; font-size: 9px; }
</style>
</head>
<body>
  <h1>{{.Title}}</h1>
  <div class="meta">
    {{if .Subtitle}}{{.Subtitle}} &middot; {{end}}{{if .Period}}Kỳ {{.Period}}{{end}}{{if .Org}} &middot; Đơn vị {{.Org}}{{end}}
  </div>
  {{if .KPI}}
  <div class="kpi">
    {{range .KPI}}<div class="card"><div class="label">{{.Label}}</div><div class="value">{{.Value}}{{if .Unit}} {{.Unit}}{{end}}</div></div>{{end}}
  </div>
  {{end}}
  {{if .Rows}}
  <table>
    <thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead>
    <tbody>
    {{range .Rows}}<tr>{{range $i, $cell := .}}<td class="{{if isNumericCell $i}}num{{end}}">{{$cell}}</td>{{end}}</tr>{{end}}
    </tbody>
  </table>
  {{end}}
  {{if .Narrative}}<div class="narrative">{{.Narrative}}</div>{{end}}
  <footer>Sinh bởi Arda statistical-service{{if .GeneratedAt}} &middot; {{.GeneratedAt}}{{end}}</footer>
</body>
</html>`

// RenderHTML renders the document to print-ready HTML (Gotenberg input).
func RenderHTML(doc *ReportDocument) ([]byte, error) {
	tmpl, err := template.New("report").Funcs(template.FuncMap{
		"isNumericCell": func(i int) bool {
			// Reuse the same heuristic the chart uses so a column is treated
			// consistently in both outputs.
			if i < 0 || i >= len(doc.Columns) {
				return false
			}
			return isValueColumn(doc.Columns[i])
		},
	}).Parse(HTMLReportTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse report template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, doc); err != nil {
		return nil, fmt.Errorf("execute report template: %w", err)
	}
	return buf.Bytes(), nil
}

// RenderXLSX writes the document as a styled workbook: title, KPI block, then
// the data table. Used both for direct download and as the editable artifact.
func RenderXLSX(doc *ReportDocument) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "BaoCao"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, fmt.Errorf("rename sheet: %w", err)
	}

	titleStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	if err != nil {
		return nil, fmt.Errorf("title style: %w", err)
	}
	headStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F2F4F8"}},
		Border: []excelize.Border{
			{Type: "left", Color: "D8D8D8", Style: 1}, {Type: "right", Color: "D8D8D8", Style: 1},
			{Type: "top", Color: "D8D8D8", Style: 1}, {Type: "bottom", Color: "D8D8D8", Style: 1},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("header style: %w", err)
	}

	row := 1
	mustSet := func(col, cellRow int, value any) error {
		cell, cerr := excelize.CoordinatesToCellName(col, cellRow)
		if cerr != nil {
			return cerr
		}
		return f.SetCellValue(sheet, cell, value)
	}
	if err := mustSet(1, row, doc.Title); err != nil {
		return nil, err
	}
	_ = f.SetCellStyle(sheet, "A1", "A1", titleStyle)
	row++

	meta := []string{}
	if doc.Subtitle != "" {
		meta = append(meta, doc.Subtitle)
	}
	if doc.Period != "" {
		meta = append(meta, "Kỳ "+doc.Period)
	}
	if doc.Org != "" {
		meta = append(meta, "Đơn vị "+doc.Org)
	}
	if len(meta) > 0 {
		if err := mustSet(1, row, strings.Join(meta, " · ")); err != nil {
			return nil, err
		}
		row++
	}
	row++ // blank line before the KPI block

	for _, k := range doc.KPI {
		if err := mustSet(1, row, k.Label); err != nil {
			return nil, err
		}
		if err := mustSet(2, row, k.Value); err != nil {
			return nil, err
		}
		if k.Unit != "" {
			if err := mustSet(3, row, k.Unit); err != nil {
				return nil, err
			}
		}
		row++
	}
	if len(doc.KPI) > 0 {
		row++
	}

	if len(doc.Columns) > 0 {
		headerRow := row
		for i, col := range doc.Columns {
			if err := mustSet(i+1, headerRow, col); err != nil {
				return nil, err
			}
		}
		if err := f.SetCellStyle(sheet,
			cellName(1, headerRow), cellName(len(doc.Columns), headerRow), headStyle); err != nil {
			return nil, fmt.Errorf("header style range: %w", err)
		}
		row++
		for _, dataRow := range doc.Rows {
			for i := range doc.Columns {
				var value any
				if i < len(dataRow) {
					value = dataRow[i]
				}
				if err := mustSet(i+1, row, value); err != nil {
					return nil, err
				}
			}
			row++
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("write workbook: %w", err)
	}
	return buf.Bytes(), nil
}

func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

// RenderReport dispatches to the requested format. PDF is produced by the
// service layer, which owns the arda-doc Gotenberg client: it calls RenderHTML
// here and hands the bytes to Gotenberg, keeping this package free of cluster
// dependencies.
func RenderReport(doc *ReportDocument, format RenderedFormat) ([]byte, string, error) {
	switch format {
	case FormatHTML:
		out, err := RenderHTML(doc)
		return out, "text/html; charset=utf-8", err
	case FormatXLSX:
		out, err := RenderXLSX(doc)
		return out, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", err
	default:
		return nil, "", fmt.Errorf("unsupported format %q", format)
	}
}
