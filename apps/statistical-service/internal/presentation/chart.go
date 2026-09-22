// Package presentation turns report/indicator results into things a user can
// look at: chart JSON the frontend renders, and print/Word/PDF documents
// rendered through arda-doc + Gotenberg.
//
// It is deliberately dumb about data: it takes rows that the report builders or
// the indicator engine already produced and shapes them for display. No SQL, no
// business rules, no HTML in the database.
package presentation

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/reports"
)

// ChartType is the subset of ECharts-compatible chart kinds this layer emits.
type ChartType string

const (
	ChartBar  ChartType = "bar"
	ChartLine ChartType = "line"
	ChartPie  ChartType = "pie"
	ChartNone ChartType = "none"
)

// Series is one numeric series of a chart.
type Series struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

// Chart is the frontend contract: enough to render an ECharts option without
// guessing, and stable enough to feed the document renderer.
type Chart struct {
	Type        ChartType `json:"type"`
	Title       string    `json:"title"`
	Categories  []string  `json:"categories"`
	Series      []Series  `json:"series,omitempty"`
	ValueFormat string    `json:"value_format,omitempty"`
	// Reason explains an automatic choice or why no chart was emitted, so the
	// UI can show it instead of a silent empty box.
	Reason string `json:"reason,omitempty"`
}

// ChartFromReport picks a chart for a report result.
//
// Rules (deterministic, not model-driven):
//   - two columns of which one is numeric and the other textual ⇒ one series
//     over the textual dimension;
//   - a column named like a period/month/year ⇒ line;
//   - ≤ 6 categories ⇒ pie, otherwise bar;
//   - any other shape ⇒ no chart, with a reason.
func ChartFromReport(title string, columns []string, rows [][]any) Chart {
	c := Chart{Type: ChartNone, Title: title}
	if len(columns) < 2 || len(rows) == 0 {
		c.Reason = "cần ít nhất 2 cột và 1 dòng dữ liệu"
		return c
	}

	labelIdx, valueIdx := -1, -1
	for i, col := range columns {
		if isLabelColumn(col) && labelIdx == -1 {
			labelIdx = i
		}
		if isValueColumn(col) && valueIdx == -1 {
			valueIdx = i
		}
	}
	if labelIdx == -1 || valueIdx == -1 {
		c.Reason = "không xác định được cột nhãn và cột số liệu"
		return c
	}

	categories := make([]string, 0, len(rows))
	values := make([]float64, 0, len(rows))
	for _, row := range rows {
		if labelIdx >= len(row) || valueIdx >= len(row) {
			continue
		}
		categories = append(categories, toString(row[labelIdx]))
		values = append(values, toFloat(row[valueIdx]))
	}
	if len(categories) == 0 {
		c.Reason = "không có dòng hợp lệ"
		return c
	}

	c.Categories = categories
	c.Series = []Series{{Name: columns[valueIdx], Values: values}}
	c.ValueFormat = formatForColumn(columns[valueIdx])

	switch {
	case isTemporalColumn(columns[labelIdx]):
		c.Type = ChartLine
	case len(categories) <= 6:
		c.Type = ChartPie
	default:
		c.Type = ChartBar
	}
	return c
}

// ChartFromIndicators charts indicator values across indicators (one bar/line
// per indicator in a category) — the KPI-overview shape.
func ChartFromIndicators(title string, series []Series, categories []string) Chart {
	c := Chart{
		Type:       ChartBar,
		Title:      title,
		Categories: categories,
		Series:     series,
	}
	if len(series) == 0 || len(categories) == 0 {
		c.Type = ChartNone
		c.Reason = "chưa có chỉ tiêu nào được tính"
	}
	return c
}

// ReportChart render options: which report + period + params.
type ReportChartRequest struct {
	Code       string
	Title      string
	PeriodCode string
	OrgCode    string
}

// Table renders report rows into the document/XLSX table shape: a header row
// followed by the data rows. Kept here so chart and document agree on order.
func Table(columns []string, rows [][]any) [][]any {
	out := make([][]any, 0, len(rows)+1)
	header := make([]any, len(columns))
	for i, col := range columns {
		header[i] = col
	}
	out = append(out, header)
	for _, row := range rows {
		values := make([]any, len(columns))
		for i := range columns {
			if i < len(row) {
				values[i] = row[i]
			}
		}
		out = append(out, values)
	}
	return out
}

func isLabelColumn(col string) bool {
	l := strings.ToLower(col)
	for _, hint := range []string{"code", "name", "period", "month", "year", "segment", "group", "status", "type"} {
		if strings.Contains(l, hint) {
			return true
		}
	}
	return false
}

func isValueColumn(col string) bool {
	l := strings.ToLower(col)
	for _, hint := range []string{"amt", "minor", "count", "balance", "rate", "ratio", "total", "value", "npl"} {
		if strings.Contains(l, hint) {
			return true
		}
	}
	return false
}

func isTemporalColumn(col string) bool {
	l := strings.ToLower(col)
	for _, hint := range []string{"period", "month", "year", "date"} {
		if strings.Contains(l, hint) {
			return true
		}
	}
	return false
}

func formatForColumn(col string) string {
	l := strings.ToLower(col)
	switch {
	case strings.Contains(l, "rate") || strings.Contains(l, "ratio"):
		return "percent"
	case strings.Contains(l, "count"):
		return "int"
	default:
		return "amount"
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}

// toFloat reads a numeric cell regardless of the driver's numeric flavour. A
// non-numeric value contributes 0 so one bad row cannot blank the chart.
func toFloat(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.ReplaceAll(x, ",", ""), "%f", &f); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}

// SortSeriesDesc orders a single-series chart by descending value, keeping
// categories and values aligned (ranked top-N reports).
func SortSeriesDesc(c Chart) Chart {
	if len(c.Series) != 1 || len(c.Categories) != len(c.Series[0].Values) {
		return c
	}
	type pair struct {
		label string
		value float64
	}
	pairs := make([]pair, len(c.Categories))
	for i := range c.Categories {
		pairs[i] = pair{c.Categories[i], c.Series[0].Values[i]}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		a, b := pairs[i].value, pairs[j].value
		if math.IsNaN(a) {
			return false
		}
		if math.IsNaN(b) {
			return true
		}
		return a > b
	})
	for i, p := range pairs {
		c.Categories[i] = p.label
		c.Series[0].Values[i] = p.value
	}
	return c
}

// Compile-time guard: the chart layer consumes the report query shape.
var _ = func(q *reports.ReportQuery) []string { return q.Columns }
