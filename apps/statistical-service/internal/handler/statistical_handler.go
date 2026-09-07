package handler

import (
	"encoding/json"
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

func writeForbiddenStat(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
}
