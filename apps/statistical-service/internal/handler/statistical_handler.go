package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	"github.com/arda-labs/arda/apps/statistical-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// StatisticalHandler exposes the QCMS HTTP surface (P2.3).
type StatisticalHandler struct {
	svc *service.StatisticalService
}

func NewStatisticalHandler(svc *service.StatisticalService) *StatisticalHandler {
	return &StatisticalHandler{svc: svc}
}

// ListReportDefinitions handles GET /api/statistical/report-definitions.
func (h *StatisticalHandler) ListReportDefinitions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	list := ardahttp.ParseListQuery(r.URL.Query())
	items, err := h.svc.ListReportDefinitions(r.Context(), repository.ListReportDefinitionsParams{
		TenantID: tenantID,
		Q:        list.Q,
		Sort:     list.Sort,
		Order:    list.Order,
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// UpsertReportDefinition handles POST /api/statistical/report-definitions.
func (h *StatisticalHandler) UpsertReportDefinition(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	var in repository.ReportDefinition
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.UpsertReportDefinition(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// ListIndicators handles GET /api/statistical/indicators.
func (h *StatisticalHandler) ListIndicators(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	list := ardahttp.ParseListQuery(r.URL.Query())
	items, err := h.svc.ListIndicators(r.Context(), repository.ListIndicatorsParams{
		TenantID: tenantID,
		Q:        list.Q,
		Sort:     list.Sort,
		Order:    list.Order,
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// UpsertIndicator handles POST /api/statistical/indicators.
func (h *StatisticalHandler) UpsertIndicator(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	var in repository.Indicator
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.UpsertIndicator(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// ListSubmissions handles GET /api/statistical/submissions.
func (h *StatisticalHandler) ListSubmissions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	list := ardahttp.ParseListQuery(r.URL.Query())
	page := list.Page
	if page < 1 {
		page = 1
	}
	items, total, err := h.svc.ListSubmissions(r.Context(), repository.ListSubmissionsParams{
		TenantID:   tenantID,
		ReportCode: r.URL.Query().Get("report_code"),
		PeriodCode: r.URL.Query().Get("period_code"),
		Status:     r.URL.Query().Get("status"),
		Sort:       list.Sort,
		Order:      list.Order,
		Page:       (page - 1) * list.PerPage,
		Size:       list.PerPage,
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeList(w, r, http.StatusOK, page, list.PerPage, total, items)
}

// CreateSubmission handles POST /api/statistical/submissions.
func (h *StatisticalHandler) CreateSubmission(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	var in repository.ReportSubmission
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.CreateSubmission(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// SubmitSubmission handles POST /api/statistical/submissions/{id}/submit.
func (h *StatisticalHandler) SubmitSubmission(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	item, err := h.svc.SubmitSubmission(r.Context(), tenantID, r.Header.Get("X-User-Id"), r.PathValue("id"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, item)
}

// RunReport handles GET /api/statistical/reports/{code}/run.
func (h *StatisticalHandler) RunReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	definition, query, rows, err := h.svc.RunReport(r.Context(), tenantID, r.PathValue("code"), reportParams(r))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"code":       definition.Code,
		"name":       definition.Name,
		"query_id":   definition.QueryID,
		"columns":    query.Columns,
		"rows":       rows,
		"row_count":  len(rows),
		"period_code": reportParams(r)["period_code"],
	})
}

// ExportReport handles GET /api/statistical/reports/{code}/export — streams
// the XLSX workbook directly (no media round-trip).
func (h *StatisticalHandler) ExportReport(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	data, filename, err := h.svc.ExportReport(r.Context(), tenantID, r.PathValue("code"), reportParams(r))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func reportParams(r *http.Request) map[string]string {
	return map[string]string{
		"period_code": r.URL.Query().Get("period_code"),
		"org_code":    r.URL.Query().Get("org_code"),
	}
}

// ListCatalogKinds handles GET /api/statistical/catalogs.
func (h *StatisticalHandler) ListCatalogKinds(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, service.CatalogKinds)
}

// ListCatalogItems handles GET /api/statistical/catalogs/{kind}.
func (h *StatisticalHandler) ListCatalogItems(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	items, err := h.svc.ListCatalogItems(r.Context(), tenantID, r.PathValue("kind"),
		r.URL.Query().Get("q"), r.URL.Query().Get("include_inactive") == "true")
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// UpsertCatalogItem handles POST /api/statistical/catalogs/{kind}.
func (h *StatisticalHandler) UpsertCatalogItem(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	var in repository.CatalogItem
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.UpsertCatalogItem(r.Context(), tenantID, r.Header.Get("X-User-Id"),
		r.PathValue("kind"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// DeactivateCatalogItem handles DELETE /api/statistical/catalogs/{kind}/{id}.
func (h *StatisticalHandler) DeactivateCatalogItem(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	if err := h.svc.SetCatalogItemActive(r.Context(), tenantID, r.PathValue("kind"),
		r.PathValue("id"), false); err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]bool{"ok": true})
}

// ListFormTemplates handles GET /api/statistical/form-templates.
func (h *StatisticalHandler) ListFormTemplates(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	items, err := h.svc.ListFormTemplates(r.Context(), tenantID, r.URL.Query().Get("include_inactive") == "true")
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// UpsertFormTemplate handles POST /api/statistical/form-templates.
func (h *StatisticalHandler) UpsertFormTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	var in repository.FormTemplate
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.UpsertFormTemplate(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// ExportFormTemplate handles GET /api/statistical/form-templates/{code}/export.
func (h *StatisticalHandler) ExportFormTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	data, filename, err := h.svc.ExportFormTemplate(r.Context(), tenantID, r.PathValue("code"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// ImportFormTemplate handles POST /api/statistical/form-templates/import.
func (h *StatisticalHandler) ImportFormTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.ImportFormTemplate(r.Context(), tenantID, r.Header.Get("X-User-Id"), payload)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// Dashboard handles GET /api/statistical/dashboard.
func (h *StatisticalHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbiddenStat(w, r)
		return
	}
	summary, err := h.svc.Dashboard(r.Context(), tenantID)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, summary)
}

func writeForbiddenStat(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
}
