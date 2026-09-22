package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/presentation"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// PresentationHandler exposes the chart + document surface for report and
// indicator results. Formats: pdf (Gotenberg), xlsx, html.
type PresentationHandler struct {
	svc presentationService
}

type presentationService interface {
	ReportChart(ctx context.Context, tenantID, code string, params map[string]string) (presentation.Chart, error)
	ReportDocument(ctx context.Context, tenantID, code string, params map[string]string) (*presentation.ReportDocument, error)
	IndicatorDocument(ctx context.Context, tenantID, period string) (*presentation.ReportDocument, error)
	RenderDocument(ctx context.Context, doc *presentation.ReportDocument, format string) ([]byte, string, error)
}

func NewPresentationHandler(svc presentationService) *PresentationHandler {
	return &PresentationHandler{svc: svc}
}

// ReportChart handles GET /api/statistical/reports/{code}/chart?period_code=.
func (h *PresentationHandler) ReportChart(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	chart, err := h.svc.ReportChart(r.Context(), tenantID, r.PathValue("code"), presentationParams(r))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, chart)
}

// ReportDocument handles GET /api/statistical/reports/{code}/document?format=.
func (h *PresentationHandler) ReportDocument(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	code := r.PathValue("code")
	doc, err := h.svc.ReportDocument(r.Context(), tenantID, code, presentationParams(r))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	h.writeRendered(w, r, doc, code)
}

// IndicatorDocument handles GET /api/statistical/indicators/document?period_code=.
func (h *PresentationHandler) IndicatorDocument(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	period := r.URL.Query().Get("period_code")
	doc, err := h.svc.IndicatorDocument(r.Context(), tenantID, period)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	}
	h.writeRendered(w, r, doc, "indicators-"+period)
}

// InternalAIReportPresentation handles
// GET /internal/ai/report-presentation for ai-service: one call returns the
// report rows plus the deterministic presentation (chart + KPI cards) so the
// assistant can answer with a KPI/chart view instead of raw JSON. The chart and
// KPI shapes come from the same presentation layer the public report endpoints
// use, so a chat chart and a downloaded document never disagree.
func (h *PresentationHandler) InternalAIReportPresentation(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "verified tenant scope is required"))
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("report_code"))
	if code == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "report_code is required"))
		return
	}
	period := strings.TrimSpace(r.URL.Query().Get("period_code"))
	if period == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "period_code is required"))
		return
	}
	doc, err := h.svc.ReportDocument(r.Context(), tenantID, code, map[string]string{
		"period_code": period,
		"org_code":    strings.TrimSpace(r.URL.Query().Get("org_code")),
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	kpis := make([]aiKPI, 0, len(doc.KPI))
	for _, k := range doc.KPI {
		kpis = append(kpis, aiKPI{Code: k.Code, Label: k.Label, Value: k.Value, Unit: k.Unit})
	}
	payload := aiReportPresentation{
		ReportCode: doc.Subtitle,
		ReportName: doc.Title,
		PeriodCode: doc.Period,
		OrgCode:    doc.Org,
		Columns:    doc.Columns,
		Rows:       doc.Rows,
		RowCount:   len(doc.Rows),
		Chart:      doc.Chart,
		KPIs:       kpis,
		Render:     "report",
	}
	if payload.ReportCode == "" {
		payload.ReportCode = code
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, payload)
}

// aiReportPresentation is the assistant-facing presentation payload: the report
// identity, the computed rows, and the chart/KPI cards the conversation can
// render. Internal wiring (query_id, param schema, tenant_id) never appears.
type aiReportPresentation struct {
	ReportCode string              `json:"report_code"`
	ReportName string              `json:"report_name"`
	PeriodCode string              `json:"period_code,omitempty"`
	OrgCode    string              `json:"org_code,omitempty"`
	Columns    []string            `json:"columns"`
	Rows       [][]any             `json:"rows"`
	RowCount   int                 `json:"row_count"`
	Chart      *presentation.Chart `json:"chart,omitempty"`
	KPIs       []aiKPI             `json:"kpis"`
	Render     string              `json:"render"`
}

// aiKPI is one headline figure; presentation.KPI carries no JSON tags (it is
// consumed by the document templates), so the wire shape is defined here.
type aiKPI struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

// writeRendered streams the rendered file with a download filename. The name
// already carries any period suffix the caller chose, so the period is only
// appended when it is not already part of the name (avoids
// "indicators-2026-09-2026-09.pdf").
func (h *PresentationHandler) writeRendered(w http.ResponseWriter, r *http.Request, doc *presentation.ReportDocument, name string) {
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "pdf"
	}
	body, contentType, err := h.svc.RenderDocument(r.Context(), doc, format)
	if err != nil {
		// An unconfigured renderer is a deployment problem, not user input.
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not configured") {
			status = http.StatusServiceUnavailable
		}
		ardahttp.WriteProblem(w, r, status, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	base := sanitizeFile(name)
	if doc.Period != "" && !strings.Contains(base, doc.Period) {
		base += "-" + doc.Period
	}
	filename := fmt.Sprintf("%s.%s", base, sanitizeFile(format))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// presentationParams collects the report run parameters shared by chart and
// document (statistical_handler already owns reportParams for its own flows).
func presentationParams(r *http.Request) map[string]string {
	params := map[string]string{
		"period_code": r.URL.Query().Get("period_code"),
		"org_code":    r.URL.Query().Get("org_code"),
	}
	return params
}

// sanitizeFile keeps the download name to a conservative character set.
func sanitizeFile(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "report"
	}
	return out
}
