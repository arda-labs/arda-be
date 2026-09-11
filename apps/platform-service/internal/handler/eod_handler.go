package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardatime "github.com/arda-labs/arda/libs/go/arda-time"
)

// EODHandler exposes the COB trigger + job definitions (P2.4).
type EODHandler struct {
	svc *service.EODService
}

func NewEODHandler(svc *service.EODService) *EODHandler {
	return &EODHandler{svc: svc}
}

// RunCOB handles POST /api/platform/eod/run?business_date=YYYY-MM-DD.
func (h *EODHandler) RunCOB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	businessDate := r.URL.Query().Get("business_date")
	if businessDate == "" {
		businessDate = ardatime.TodayCtx(r.Context())
	}
	result, err := h.svc.Run(r.Context(), tenantID, businessDate)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, result)
}

// SeedCOBJobs handles POST /api/platform/eod/seed.
func (h *EODHandler) SeedCOBJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	if err := h.svc.SeedJobs(r.Context(), tenantID); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]bool{"seeded": true})
}

// ListJobs handles GET /api/platform/jobs.
func (h *EODHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	jobs, err := h.svc.ListJobs(r.Context(), tenantID)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, jobs)
}

// ListJobRuns handles GET /api/platform/jobs/runs.
func (h *EODHandler) ListJobRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	runs, err := h.svc.ListJobRuns(r.Context(), tenantID, r.URL.Query().Get("job_code"), limit)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, runs)
}
