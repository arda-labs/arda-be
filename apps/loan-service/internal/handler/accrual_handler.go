package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// AccrualHandler exposes the EOD accrual job endpoint (called by the EOD
// engine / platform jobs with a service identity) and the accrual read API
// for the loan remote.
type AccrualHandler struct {
	svc *service.AccrualService
}

func NewAccrualHandler(svc *service.AccrualService) *AccrualHandler {
	return &AccrualHandler{svc: svc}
}

// RunDailyAccrual handles POST /internal/jobs/accrual-daily?to_date=YYYY-MM-DD.
// Tenant comes from X-Tenant-Id forwarded by the caller; actor is the job id.
func (h *AccrualHandler) RunDailyAccrual(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	toDate := r.URL.Query().Get("to_date")
	actor := strings.TrimSpace(r.Header.Get("X-User-Id"))
	if actor == "" {
		actor = "eod-job"
	}
	result, err := h.svc.RunDaily(r.Context(), tenantID, toDate, actor)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, result)
}

// ListAccruals handles GET /api/loan/accruals.
func (h *AccrualHandler) ListAccruals(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	items, err := h.svc.ListAccruals(r.Context(), tenantID, 200)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}
