package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// CashHandler exposes VCM cash endpoints (P2.4b).
type CashHandler struct {
	svc *service.CashService
}

func NewCashHandler(svc *service.CashService) *CashHandler {
	return &CashHandler{svc: svc}
}

// RecordCash handles POST /api/finance/cash.
func (h *CashHandler) RecordCash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in service.CashTxnInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	in.Actor = r.Header.Get("X-User-Id")
	in.OrgCode = r.Header.Get("X-Org-Id")
	created, err := h.svc.Record(r.Context(), tenantID, &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// ListCash handles GET /api/finance/cash (W7).
func (h *CashHandler) ListCash(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	q := r.URL.Query()
	rows, err := h.svc.List(r.Context(), tenantID, q.Get("from"), q.Get("to"), q.Get("direction"))
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, rows)
}

// Position handles GET /api/finance/cash-position.
func (h *CashHandler) Position(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	rows, err := h.svc.Position(r.Context(), tenantID)
	if err != nil {
		ardahttp.WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, err.Error()))
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"rows": rows})
}
