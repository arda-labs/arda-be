package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// chartMaxCategories and chartMaxSeries bound the payload the model can hand to
// the interface, so one tool call cannot build an unbounded chart. The model
// only describes the data; the frontend owns the rendering.
const (
	chartMaxCategories = 200
	chartMaxSeries     = 5
)

type chartArguments struct {
	Title       string   `json:"title"`
	ChartType   string   `json:"chart_type"`
	Categories  []string `json:"categories"`
	ValueFormat string   `json:"value_format"`
	// Optional report metadata: when the chart comes from a catalogued report,
	// echoing the code/period lets the interface offer the Excel/PDF download.
	ReportCode string `json:"report_code"`
	PeriodCode string `json:"period_code"`
	OrgCode    string `json:"org_code"`
	Series     []struct {
		Name   string    `json:"name"`
		Values []float64 `json:"values"`
	} `json:"series"`
}

// ChartMetaTool exposes renderChart: the model hands over rows it already
// computed (typically from execute()) as a small, validated chart description,
// and the interface draws the chart, KPI-free table and values. This is the
// ad-hoc path; catalogued reports go through arda.statistical.getReportPresentation
// so their chart/KPI come from the deterministic presentation layer.
type ChartMetaTool struct{}

func NewChartMetaTool() *ChartMetaTool { return &ChartMetaTool{} }

func (t *ChartMetaTool) Definition() Definition {
	return Definition{
		Name:    "renderChart",
		Version: 1,
		Kind:    "read",
		Risk:    "low",
		Description: "Render a chart and its data table for the user from rows you already computed (for example the result of execute()). " +
			"Use this for ad-hoc analysis that is not a catalogued report; for catalogued reports call arda.statistical.getReportPresentation instead. " +
			"The interface draws the chart — you only describe the data, never ECharts options or HTML.",
		Timeout: 1 * time.Second,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"title": { "type": "string", "description": "Chart title in Vietnamese." },
				"chart_type": { "type": "string", "enum": ["bar", "line", "pie"], "description": "Chart kind." },
				"categories": {
					"type": "array",
					"items": { "type": "string" },
					"description": "Category labels (x-axis values, or slice names for a pie chart)."
				},
				"series": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"name": { "type": "string", "description": "Series name in Vietnamese." },
							"values": { "type": "array", "items": { "type": "number" } }
						},
						"required": ["values"]
					},
					"description": "One or more numeric series; each must align with categories."
				},
				"value_format": {
					"type": "string",
					"enum": ["amount", "percent", "int", "number"],
					"description": "How the interface formats the values."
				},
				"report_code": {
					"type": "string",
					"description": "Optional: report code when this chart comes from a catalogued report (enables the Excel/PDF download)."
				},
				"period_code": {
					"type": "string",
					"description": "Optional: reporting period YYYY-MM for the download."
				},
				"org_code": {
					"type": "string",
					"description": "Optional: org unit filter for the download."
				}
			},
			"required": ["title", "chart_type", "categories", "series"]
		}`),
	}
}

func (t *ChartMetaTool) Execute(ctx context.Context, scope Context, arguments json.RawMessage) (Result, error) {
	_ = ctx

	var input chartArguments
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		return Result{}, fmt.Errorf("%w: title is required", ErrInvalidArgument)
	}
	chartType := strings.ToLower(strings.TrimSpace(input.ChartType))
	switch chartType {
	case "bar", "line", "pie":
	default:
		return Result{}, fmt.Errorf("%w: chart_type must be one of bar, line, pie", ErrInvalidArgument)
	}
	if len(input.Categories) == 0 || len(input.Categories) > chartMaxCategories {
		return Result{}, fmt.Errorf("%w: categories must have 1-%d items", ErrInvalidArgument, chartMaxCategories)
	}
	if len(input.Series) == 0 || len(input.Series) > chartMaxSeries {
		return Result{}, fmt.Errorf("%w: series must have 1-%d items", ErrInvalidArgument, chartMaxSeries)
	}
	for index, series := range input.Series {
		if len(series.Values) != len(input.Categories) {
			return Result{}, fmt.Errorf("%w: series %d has %d values but there are %d categories",
				ErrInvalidArgument, index+1, len(series.Values), len(input.Categories))
		}
	}
	valueFormat := strings.ToLower(strings.TrimSpace(input.ValueFormat))
	switch valueFormat {
	case "", "amount", "percent", "int", "number":
	default:
		return Result{}, fmt.Errorf("%w: value_format must be one of amount, percent, int, number", ErrInvalidArgument)
	}
	if valueFormat == "" {
		valueFormat = "number"
	}

	// The presentation shape below matches the statistical report-presentation
	// contract, so the same frontend card renders both a catalogued report and
	// an ad-hoc chart. report_code/period_code stay empty: this is not a
	// catalogued report and no document download applies.
	columns := make([]string, 0, len(input.Series)+1)
	columns = append(columns, "Danh mục")
	seriesPayload := make([]map[string]any, 0, len(input.Series))
	for index, series := range input.Series {
		name := strings.TrimSpace(series.Name)
		if name == "" {
			name = fmt.Sprintf("Chuỗi %d", index+1)
		}
		columns = append(columns, name)
		seriesPayload = append(seriesPayload, map[string]any{
			"name":   name,
			"values": series.Values,
		})
	}
	rows := make([][]any, 0, len(input.Categories))
	for categoryIndex, category := range input.Categories {
		row := make([]any, 0, len(input.Series)+1)
		row = append(row, category)
		for _, series := range input.Series {
			row = append(row, series.Values[categoryIndex])
		}
		rows = append(rows, row)
	}

	payload := map[string]any{
		"render":      "report",
		"report_code": strings.TrimSpace(input.ReportCode),
		"report_name": title,
		"period_code": strings.TrimSpace(input.PeriodCode),
		"org_code":    strings.TrimSpace(input.OrgCode),
		"columns":     columns,
		"rows":        rows,
		"row_count":   len(rows),
		"chart": map[string]any{
			"type":         chartType,
			"title":        title,
			"categories":   input.Categories,
			"series":       seriesPayload,
			"value_format": valueFormat,
		},
		"kpis": []any{},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("encode chart: %w", err)
	}
	return Result{
		Data:      raw,
		Summary:   fmt.Sprintf("Rendered a %s chart with %d categories.", chartType, len(input.Categories)),
		Source:    "ai-chart",
		RequestID: scope.RequestID,
		FreshAt:   time.Now().UTC(),
	}, nil
}
