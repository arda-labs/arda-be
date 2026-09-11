package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// PlanHandler exposes the loan plan catalog (W7).
type PlanHandler struct {
	svc *service.PlanService
}

func NewPlanHandler(svc *service.PlanService) *PlanHandler {
	return &PlanHandler{svc: svc}
}

// Plans handles GET/POST /api/loan/plans.
func (h *PlanHandler) Plans(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenPlan(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.svc.ListPlans(r.Context(), tenantID, r.URL.Query().Get("status"))
		if err != nil {
			ardahttp.WriteServiceError(w, r, err)
			return
		}
		ardahttp.WriteEnvelopeUnpaged(w, r, items)
	case http.MethodPost, http.MethodPut:
		var in repository.LoanPlan
		if !decodeBody(w, r, &in) {
			return
		}
		created, err := h.svc.UpsertPlan(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
		if err != nil {
			ardahttp.WriteServiceError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
	default:
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
	}
}

// PlanByID handles DELETE /api/loan/plans/{id}.
func (h *PlanHandler) PlanByID(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenPlan(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	if err := h.svc.ClosePlan(r.Context(), tenantID, r.PathValue("id")); err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func writeForbiddenPlan(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
}
