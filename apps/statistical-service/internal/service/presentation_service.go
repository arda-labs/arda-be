package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/presentation"
	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	ardadoc "github.com/arda-labs/arda/libs/go/arda-doc"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// presentationService renders report/indicator results into user-facing output
// (chart JSON, HTML, XLSX, PDF). PDF goes through arda-doc's Gotenberg client;
// when Gotenberg is not configured the caller gets a clear error instead of a
// silently missing file.
type presentationService struct {
	statistical *StatisticalService
	gotenberg   *ardadoc.GotenbergClient
}

func newPresentationService(statistical *StatisticalService, gotenbergURL string) (*presentationService, error) {
	svc := &presentationService{statistical: statistical}
	if strings.TrimSpace(gotenbergURL) != "" {
		client, err := ardadoc.NewGotenbergClient(gotenbergURL)
		if err != nil {
			return nil, fmt.Errorf("gotenberg client: %w", err)
		}
		svc.gotenberg = client
	}
	return svc, nil
}

// ReportChart returns the chart contract for one report run.
func (s *presentationService) ReportChart(ctx context.Context, tenantID, code string, params map[string]string) (presentation.Chart, error) {
	definition, query, rows, err := s.statistical.RunReport(ctx, tenantID, code, params)
	if err != nil {
		return presentation.Chart{}, err
	}
	chart := presentation.ChartFromReport(definition.Name, query.Columns, rows)
	// Ranked reports read better sorted; leave time series in period order.
	if chart.Type != presentation.ChartLine {
		chart = presentation.SortSeriesDesc(chart)
	}
	return chart, nil
}

// ReportDocument assembles the printable document for one report run. KPI cards
// come from stored indicator results for the same period, so the document and
// the indicator API never disagree.
func (s *presentationService) ReportDocument(ctx context.Context, tenantID, code string, params map[string]string) (*presentation.ReportDocument, error) {
	definition, query, rows, err := s.statistical.RunReport(ctx, tenantID, code, params)
	if err != nil {
		return nil, err
	}
	period := params["period_code"]
	doc := &presentation.ReportDocument{
		Title:       definition.Name,
		Subtitle:    definition.Code,
		Period:      period,
		Org:         params["org_code"],
		Columns:     query.Columns,
		Rows:        rows,
		GeneratedAt: ardatime.Now().Format("2006-01-02 15:04"),
	}
	if period != "" {
		kpi, err := s.periodKPI(ctx, tenantID, period, definition.GroupCode)
		if err != nil {
			return nil, err
		}
		doc.KPI = kpi
	}
	chart := presentation.ChartFromReport(definition.Name, query.Columns, rows)
	if chart.Type != presentation.ChartNone {
		doc.Chart = &chart
	}
	return doc, nil
}

// IndicatorDocument renders stored indicator values for a period as a
// document: one row per indicator with its computed value.
func (s *presentationService) IndicatorDocument(ctx context.Context, tenantID, period string) (*presentation.ReportDocument, error) {
	if period == "" {
		return nil, errors.New("period_code is required")
	}
	results, err := s.statistical.ListIndicatorResults(ctx, repository.ListIndicatorResultsParams{
		TenantID: tenantID, PeriodCode: period,
	})
	if err != nil {
		return nil, err
	}
	indicators, err := s.statistical.ListIndicators(ctx, repository.ListIndicatorsParams{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	nameByCode := make(map[string]repository.Indicator, len(indicators))
	for _, ind := range indicators {
		nameByCode[ind.Code] = ind
	}

	rows := make([][]any, 0, len(results))
	for _, r := range results {
		label := r.IndicatorCode
		unit := ""
		group := ""
		if ind, ok := nameByCode[r.IndicatorCode]; ok {
			label = ind.Name
			unit = ind.Unit
			group = ind.GroupCode
		}
		rows = append(rows, []any{r.IndicatorCode, label, group, r.DimensionKey, valueText(r.Value), unit})
	}
	return &presentation.ReportDocument{
		Title:       "Chỉ tiêu QCMS theo kỳ",
		Subtitle:    "statistical-service",
		Period:      period,
		Columns:     []string{"Mã chỉ tiêu", "Tên chỉ tiêu", "Nhóm", "Chiều phân tích", "Giá trị", "Đơn vị"},
		Rows:        rows,
		GeneratedAt: ardatime.Now().Format("2006-01-02 15:04"),
	}, nil
}

// RenderDocument renders a document in the requested format. PDF is produced by
// Gotenberg from the same HTML, so HTML and PDF never drift.
func (s *presentationService) RenderDocument(ctx context.Context, doc *presentation.ReportDocument, format string) ([]byte, string, error) {
	switch strings.ToLower(format) {
	case "", "pdf":
		if s.gotenberg == nil {
			return nil, "", errors.New("PDF rendering is not configured (GOTENBERG_URL is empty)")
		}
		html, err := presentation.RenderHTML(doc)
		if err != nil {
			return nil, "", err
		}
		pdf, err := s.gotenberg.HTMLToPDF(ctx, html, &ardadoc.PDFOptions{PaperWidth: "8.27in", PaperHeight: "11.69in"})
		if err != nil {
			return nil, "", fmt.Errorf("render pdf: %w", err)
		}
		return pdf, ardadoc.PDFMime, nil
	case "xlsx":
		return presentation.RenderReport(doc, presentation.FormatXLSX)
	case "html":
		return presentation.RenderReport(doc, presentation.FormatHTML)
	default:
		return nil, "", fmt.Errorf("unsupported format %q (use pdf, xlsx or html)", format)
	}
}

// maxReportKPI bounds the headline cards on a report so a single document does
// not turn into a wall of every indicator the tenant has computed.
const maxReportKPI = 8

// reportKPIGroups maps a report definition's group code to the Vietnamese
// indicator groups that belong to the same business area. Reports group by
// code (LNM/DPM/CFM/CRM/OPS) while indicators group by name, so the two are
// bridged here; an unmapped group falls back to every indicator.
var reportKPIGroups = map[string][]string{
	"LNM": {"Tín dụng", "TSĐB"},
	"DPM": {"Huy động vốn"},
	"CFM": {"Góp vốn cổ phần", "Nguồn vốn"},
	"CRM": {"Khách hàng"},
	"OPS": {},
}

// reportKpiFilter returns the indicator groups a report's cards may use and
// whether a filter applies. An empty (non-nil) list means "this report has no
// matching indicator group"; a nil list means no filtering.
func reportKpiFilter(reportGroup string) ([]string, bool) {
	group := strings.ToUpper(strings.TrimSpace(reportGroup))
	if group == "" {
		return nil, false
	}
	mapped, ok := reportKPIGroups[group]
	if !ok {
		return nil, false
	}
	return mapped, true
}

// periodKPI lists stored indicator values for the period as KPI cards, narrowed
// to the report's business area and capped so the card stays readable.
func (s *presentationService) periodKPI(ctx context.Context, tenantID, period, reportGroup string) ([]presentation.KPI, error) {
	results, err := s.statistical.ListIndicatorResults(ctx, repository.ListIndicatorResultsParams{
		TenantID: tenantID, PeriodCode: period,
	})
	if err != nil {
		return nil, err
	}
	indicators, err := s.statistical.ListIndicators(ctx, repository.ListIndicatorsParams{TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	byCode := make(map[string]repository.Indicator, len(indicators))
	for _, ind := range indicators {
		byCode[ind.Code] = ind
	}
	groups, filtered := reportKpiFilter(reportGroup)

	kpi := make([]presentation.KPI, 0, maxReportKPI)
	for _, r := range results {
		if r.DimensionKey != "" {
			continue // totals only on the headline cards
		}
		ind, known := byCode[r.IndicatorCode]
		if filtered && (!known || !containsString(groups, ind.GroupCode)) {
			continue
		}
		kpi = append(kpi, presentation.KPI{
			Code:  r.IndicatorCode,
			Label: firstNonEmpty(ind.Name, r.IndicatorCode),
			Value: valueText(r.Value),
			Unit:  ind.Unit,
		})
		if len(kpi) >= maxReportKPI {
			break
		}
	}
	return kpi, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func valueText(v *float64) string {
	if v == nil {
		return ""
	}
	// Trim trailing zeros so amounts read cleanly (10000000000 → 1e+10 only if
	// we used %g, so format explicitly).
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", *v), "0"), ".")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
