package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/statistical-service/internal/reports"
	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// aiMaxQueryLen caps the free-text search the assistant can submit; it matches
// the q parameter schema in contracts/ai-internal/statistical-v1.json.
const aiMaxQueryLen = 128

// aiListSpec is the AI tool list contract: per_page 1..20 with default 10.
// all=1, views and sort are rejected so the assistant can never pull
// unbounded pages or reorder a catalog behind its back.
var aiListSpec = ardahttp.ListSpec{
	DefaultPerPage: 10,
	MaxPerPage:     20,
}

// aiSubmissionStatuses mirrors the rpt_report_submissions lifecycle. The
// contract declares the same enum so an invalid status is rejected twice: in
// the sandbox argument validation and again here for direct HTTP callers.
var aiSubmissionStatuses = []string{"DRAFT", "SUBMITTED", "APPROVED", "REJECTED"}

// aiStatisticalSource is the read slice of StatisticalService the AI surface
// needs. The interface form keeps the handler testable without a database
// while the concrete *service.StatisticalService satisfies it as-is.
type aiStatisticalSource interface {
	ListReportDefinitions(ctx context.Context, params repository.ListReportDefinitionsParams) ([]repository.ReportDefinition, error)
	ListIndicators(ctx context.Context, params repository.ListIndicatorsParams) ([]repository.Indicator, error)
	ListSubmissions(ctx context.Context, params repository.ListSubmissionsParams) ([]repository.ReportSubmission, int, error)
	ListIndicatorResults(ctx context.Context, params repository.ListIndicatorResultsParams) ([]repository.IndicatorResult, error)
	ListAlerts(ctx context.Context, tenantID, status, periodCode string) ([]repository.IndicatorAlert, error)
	RunReport(ctx context.Context, tenantID, code string, params map[string]string) (*repository.ReportDefinition, *reports.ReportQuery, [][]any, error)
}

// InternalAIHandler serves the /internal/ai/* surface consumed by ai-service.
// The signed caller assertion is verified by the router's internalAIService
// middleware; the delegated subject (X-Tenant-Id) is re-validated here and
// forwarded to the repository, so a tenant can never see another tenant's
// report metadata. Raw report payloads, query ids, param schemas and
// submission bodies never leave this handler.
type InternalAIHandler struct {
	source aiStatisticalSource
}

func NewInternalAIHandler(source aiStatisticalSource) *InternalAIHandler {
	return &InternalAIHandler{source: source}
}

// InternalAIListReportDefinitions serves GET /internal/ai/report-definitions
// for ai-service. It exposes catalog metadata only: code, name, group, output
// format and is_active. query_id (internal builder key), param_schema (raw
// JSON configuration), template_file_id and actor/timestamps are dropped.
func (h *InternalAIHandler) InternalAIListReportDefinitions(w http.ResponseWriter, r *http.Request) {
	tenantID, listReq, ok := aiListRequest(w, r)
	if !ok {
		return
	}
	items, err := h.source.ListReportDefinitions(r.Context(), repository.ListReportDefinitionsParams{
		TenantID: tenantID,
		Q:        aiQuery(r),
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	res := ardahttp.PageSlice(items, listReq.ListQuery)
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(res.Page, res.PerPage, res.Total, toAIReportDefinitions(res.Items)))
}

// InternalAIListIndicators serves GET /internal/ai/indicators for ai-service.
// The indicator catalog is small reference metadata; tenant_id, actor ids and
// timestamps are dropped by toAIIndicators.
func (h *InternalAIHandler) InternalAIListIndicators(w http.ResponseWriter, r *http.Request) {
	tenantID, listReq, ok := aiListRequest(w, r)
	if !ok {
		return
	}
	items, err := h.source.ListIndicators(r.Context(), repository.ListIndicatorsParams{
		TenantID: tenantID,
		Q:        aiQuery(r),
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	res := ardahttp.PageSlice(items, listReq.ListQuery)
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(res.Page, res.PerPage, res.Total, toAIIndicators(res.Items)))
}

// InternalAIListSubmissions serves GET /internal/ai/submissions for
// ai-service. It exposes the maker-checker status of report periods only: the
// raw payload (report data), workflow case, submitter identity and timestamps
// are dropped. Filters are structured (report_code/period_code/status) rather
// than free text because the repository query is structured too.
func (h *InternalAIHandler) InternalAIListSubmissions(w http.ResponseWriter, r *http.Request) {
	tenantID, listReq, ok := aiListRequest(w, r)
	if !ok {
		return
	}
	status, ok := aiSubmissionStatus(w, r)
	if !ok {
		return
	}
	page := listReq.Page
	if page < 1 {
		page = 1
	}
	items, total, err := h.source.ListSubmissions(r.Context(), repository.ListSubmissionsParams{
		TenantID:   tenantID,
		ReportCode: aiQueryParam(r, "report_code"),
		PeriodCode: aiQueryParam(r, "period_code"),
		Status:     status,
		Page:       (page - 1) * listReq.PerPage,
		Size:       listReq.PerPage,
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, listReq.PerPage, total, toAISubmissions(items)))
}

// InternalAIRunReport serves GET /internal/ai/report-run for ai-service: run
// one catalogued report (identified by code, never by SQL) for a period and
// return the already-redacted rows. This is the NL-routing target — the model
// picks a report code + parameters, and the service does the rest.
func (h *InternalAIHandler) InternalAIRunReport(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := aiGetTenant(w, r)
	if !ok {
		return
	}
	code := aiQueryParam(r, "report_code")
	if code == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "report_code is required"))
		return
	}
	period := aiQueryParam(r, "period_code")
	if period == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "period_code is required"))
		return
	}
	definition, query, rows, err := h.source.RunReport(r.Context(), tenantID, code, map[string]string{
		"period_code": period,
		"org_code":    aiQueryParam(r, "org_code"),
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, toAIReportRun(definition, query.Columns, rows))
}

// InternalAIListIndicatorResults serves GET /internal/ai/indicator-results for
// ai-service: the stored values of computed indicators for one period. Only
// computed results are exposed — the formula, sources and dimensions JSON stay
// behind this boundary.
func (h *InternalAIHandler) InternalAIListIndicatorResults(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := aiGetTenant(w, r)
	if !ok {
		return
	}
	results, err := h.source.ListIndicatorResults(r.Context(), repository.ListIndicatorResultsParams{
		TenantID:      tenantID,
		PeriodCode:    aiQueryParam(r, "period_code"),
		IndicatorCode: aiQueryParam(r, "indicator_code"),
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	// Labels come from the catalog so the assistant can answer with names.
	indicators, err := h.source.ListIndicators(r.Context(), repository.ListIndicatorsParams{TenantID: tenantID})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	res := aiPageSlice(results, aiResultLimit(r))
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(res.page, res.perPage, res.total, toAIIndicatorResults(res.items, indicators)))
}

// InternalAIListIndicatorAlerts serves GET /internal/ai/indicator-alerts for
// ai-service: the threshold alerts the proactive loop raised. Rule
// configuration and its owner stay behind this boundary — the assistant only
// needs what breached, by how much and how severe.
func (h *InternalAIHandler) InternalAIListIndicatorAlerts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := aiGetTenant(w, r)
	if !ok {
		return
	}
	alerts, err := h.source.ListAlerts(r.Context(), tenantID,
		strings.ToUpper(strings.TrimSpace(aiQueryParam(r, "status"))),
		aiQueryParam(r, "period_code"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	res := aiPageSlice(alerts, aiResultLimit(r))
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(res.page, res.perPage, res.total, toAIIndicatorAlerts(res.items)))
}

// aiIndicatorAlert is the assistant-facing alert row. The rule's owner,
// activity flag and timestamps are dropped.
type aiIndicatorAlert struct {
	RuleCode      string   `json:"rule_code"`
	IndicatorCode string   `json:"indicator_code"`
	PeriodCode    string   `json:"period_code"`
	DimensionKey  string   `json:"dimension_key,omitempty"`
	Value         *float64 `json:"value,omitempty"`
	Threshold     float64  `json:"threshold"`
	Operator      string   `json:"operator"`
	Severity      string   `json:"severity"`
	Status        string   `json:"status"`
	Message       string   `json:"message,omitempty"`
}

func toAIIndicatorAlerts(items []repository.IndicatorAlert) []aiIndicatorAlert {
	out := make([]aiIndicatorAlert, 0, len(items))
	for _, a := range items {
		out = append(out, aiIndicatorAlert{
			RuleCode:      a.RuleCode,
			IndicatorCode: a.IndicatorCode,
			PeriodCode:    a.PeriodCode,
			DimensionKey:  a.DimensionKey,
			Value:         a.Value,
			Threshold:     a.Threshold,
			Operator:      a.Operator,
			Severity:      a.Severity,
			Status:        a.Status,
			Message:       a.Message,
		})
	}
	return out
}

// aiGetTenant reads the delegated tenant for non-list AI reads.
func aiGetTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return "", false
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "verified tenant scope is required"))
		return "", false
	}
	return tenantID, true
}

// aiResultLimit bounds the indicator-result page (ai-service never asks for an
// unbounded slice).
func aiResultLimit(r *http.Request) int {
	limit := aiListSpec.DefaultPerPage
	if raw := strings.TrimSpace(r.URL.Query().Get("per_page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= aiListSpec.MaxPerPage {
			limit = n
		}
	}
	return limit
}

func aiPageSlice[T any](items []T, limit int) struct {
	items   []T
	page    int
	perPage int
	total   int
} {
	total := len(items)
	if limit <= 0 {
		limit = aiListSpec.DefaultPerPage
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return struct {
		items   []T
		page    int
		perPage int
		total   int
	}{items: items, page: 1, perPage: limit, total: total}
}

// aiReportRun is the assistant-facing report result: the catalog metadata plus
// the rows. tenant_id and the internal query wiring (query_id, param schema)
// are never included.
type aiReportRun struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	PeriodCode string   `json:"period_code"`
	Columns    []string `json:"columns"`
	Rows       [][]any  `json:"rows"`
	RowCount   int      `json:"row_count"`
}

func toAIReportRun(def *repository.ReportDefinition, columns []string, rows [][]any) aiReportRun {
	out := aiReportRun{Columns: columns, Rows: rows, RowCount: len(rows)}
	if def != nil {
		out.Code = def.Code
		out.Name = def.Name
	}
	return out
}

// aiIndicatorResult is the assistant-facing computed indicator value. The
// formula/sources/dimensions configuration is not exposed; the dimension key
// survives because it tells the assistant which slice the value covers.
type aiIndicatorResult struct {
	IndicatorCode string   `json:"indicator_code"`
	Name          string   `json:"name,omitempty"`
	Unit          string   `json:"unit,omitempty"`
	PeriodCode    string   `json:"period_code"`
	DimensionKey  string   `json:"dimension_key,omitempty"`
	Value         *float64 `json:"value,omitempty"`
	Source        string   `json:"source"`
}

func toAIIndicatorResults(items []repository.IndicatorResult, catalog []repository.Indicator) []aiIndicatorResult {
	names := make(map[string]repository.Indicator, len(catalog))
	for _, ind := range catalog {
		names[ind.Code] = ind
	}
	out := make([]aiIndicatorResult, 0, len(items))
	for _, item := range items {
		row := aiIndicatorResult{
			IndicatorCode: item.IndicatorCode,
			PeriodCode:    item.PeriodCode,
			DimensionKey:  item.DimensionKey,
			Value:         item.Value,
			Source:        item.Source,
		}
		if ind, ok := names[item.IndicatorCode]; ok {
			row.Name = ind.Name
			row.Unit = ind.Unit
		}
		out = append(out, row)
	}
	return out
}

// aiListRequest enforces the shared AI list preconditions: GET only, the
// verified tenant from the delegated X-Tenant-Id header and a bounded
// page/per_page/q query. The tenant is never read from the query string.
func aiListRequest(w http.ResponseWriter, r *http.Request) (string, ardahttp.ListRequest, bool) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return "", ardahttp.ListRequest{}, false
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "verified tenant scope is required"))
		return "", ardahttp.ListRequest{}, false
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), aiListSpec)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return "", ardahttp.ListRequest{}, false
	}
	return tenantID, listReq, true
}

// aiSubmissionStatus normalizes the optional status filter and rejects values
// outside the submission lifecycle.
func aiSubmissionStatus(w http.ResponseWriter, r *http.Request) (string, bool) {
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		return "", true
	}
	for _, known := range aiSubmissionStatuses {
		if status == known {
			return status, true
		}
	}
	ardahttp.WriteProblem(w, r, http.StatusBadRequest,
		ardaerrors.New(ardaerrors.CodeInvalidInput, "status must be one of: "+strings.Join(aiSubmissionStatuses, ", ")))
	return "", false
}

// aiQuery trims the free-text search and clamps it to aiMaxQueryLen
// (rune-safe: the query can carry Vietnamese text).
func aiQuery(r *http.Request) string {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	runes := []rune(q)
	if len(runes) > aiMaxQueryLen {
		return string(runes[:aiMaxQueryLen])
	}
	return q
}

// aiQueryParam trims and clamps one structured filter parameter; the length is
// bounded even though the repository treats it as a literal substring.
func aiQueryParam(r *http.Request, key string) string {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	runes := []rune(value)
	if len(runes) > aiMaxQueryLen {
		return string(runes[:aiMaxQueryLen])
	}
	return value
}

// aiReportDefinition is the redacted report-definition metadata exposed to the
// AI SDK. query_id, param_schema, template_file_id, tenant_id, actor ids and
// timestamps are dropped here and again by the response allowlist in
// contracts/ai-internal/statistical-v1.json.
type aiReportDefinition struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	GroupCode    string `json:"group_code,omitempty"`
	OutputFormat string `json:"output_format"`
	IsActive     bool   `json:"is_active"`
}

func toAIReportDefinitions(items []repository.ReportDefinition) []aiReportDefinition {
	redacted := make([]aiReportDefinition, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiReportDefinition{
			ID:           item.ID,
			Code:         item.Code,
			Name:         item.Name,
			GroupCode:    item.GroupCode,
			OutputFormat: item.OutputFormat,
			IsActive:     item.IsActive,
		})
	}
	return redacted
}

// aiIndicator is the redacted indicator metadata exposed to the AI SDK.
type aiIndicator struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Unit      string `json:"unit,omitempty"`
	GroupCode string `json:"group_code,omitempty"`
	IsActive  bool   `json:"is_active"`
}

func toAIIndicators(items []repository.Indicator) []aiIndicator {
	redacted := make([]aiIndicator, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiIndicator{
			ID:        item.ID,
			Code:      item.Code,
			Name:      item.Name,
			Unit:      item.Unit,
			GroupCode: item.GroupCode,
			IsActive:  item.IsActive,
		})
	}
	return redacted
}

// aiSubmission is the redacted submission-status shape exposed to the AI SDK.
// The raw payload, workflow case, submitted_by/created_by identities and
// timestamps are dropped; submitted_at survives as the maker-checker date.
type aiSubmission struct {
	ID          string     `json:"id"`
	ReportCode  string     `json:"report_code"`
	PeriodCode  string     `json:"period_code"`
	Status      string     `json:"status"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
}

func toAISubmissions(items []repository.ReportSubmission) []aiSubmission {
	redacted := make([]aiSubmission, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiSubmission{
			ID:          item.ID,
			ReportCode:  item.ReportCode,
			PeriodCode:  item.PeriodCode,
			Status:      item.Status,
			SubmittedAt: item.SubmittedAt,
		})
	}
	return redacted
}
