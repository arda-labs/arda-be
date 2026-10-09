package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type AgreementDailySnapshotHandler struct {
	svc *service.AgreementDailySnapshotService
}

func NewAgreementDailySnapshotHandler(svc *service.AgreementDailySnapshotService) *AgreementDailySnapshotHandler {
	return &AgreementDailySnapshotHandler{svc: svc}
}

// RunDaily handles POST /internal/jobs/agreement-daily-snapshot?to_date=YYYY-MM-DD.
func (h *AgreementDailySnapshotHandler) RunDaily(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	result, err := h.svc.Run(r.Context(), tenantID, r.URL.Query().Get("to_date"))
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, result)
}
