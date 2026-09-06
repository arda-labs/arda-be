package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// ProvisionHandler exposes the EOD provision batch endpoint.
type ProvisionHandler struct {
	svc *service.ProvisionService
}

func NewProvisionHandler(svc *service.ProvisionService) *ProvisionHandler {
	return &ProvisionHandler{svc: svc}
}

// RunProvision handles POST /internal/jobs/provision-daily?to_date=YYYY-MM-DD.
func (h *ProvisionHandler) RunProvision(w http.ResponseWriter, r *http.Request) {
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
	result, err := h.svc.Run(r.Context(), tenantID, toDate, actor)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, result)
}
