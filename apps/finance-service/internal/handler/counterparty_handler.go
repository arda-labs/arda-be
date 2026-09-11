package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// CounterpartyHandler exposes the partner master surface (W4c-E).
type CounterpartyHandler struct {
	svc *service.CounterpartyService
}

func NewCounterpartyHandler(svc *service.CounterpartyService) *CounterpartyHandler {
	return &CounterpartyHandler{svc: svc}
}

// Counterparties handles GET/POST /api/finance/counterparties.
func (h *CounterpartyHandler) Counterparties(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenCP(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.svc.List(r.Context(), tenantID, r.URL.Query().Get("q"),
			r.URL.Query().Get("party_type"), r.URL.Query().Get("include_inactive") == "true")
		if err != nil {
			ardahttp.WriteServiceError(w, r, err)
			return
		}
		ardahttp.WriteEnvelopeUnpaged(w, r, items)
	case http.MethodPost:
		var in repository.Counterparty
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeInvalidBodyCP(w, r)
			return
		}
		created, err := h.svc.Upsert(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
		if err != nil {
			ardahttp.WriteServiceError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
	default:
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
	}
}

// CounterpartyByID handles PUT/DELETE /api/finance/counterparties/{id} and
// GET/POST /api/finance/counterparties/{id}/accounts.
func (h *CounterpartyHandler) CounterpartyByID(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeForbiddenCP(w, r)
		return
	}
	id := r.PathValue("id")
	if strings.HasSuffix(r.URL.Path, "/accounts") {
		switch r.Method {
		case http.MethodGet:
			items, err := h.svc.ListAccounts(r.Context(), tenantID, id)
			if err != nil {
				ardahttp.WriteServiceError(w, r, err)
				return
			}
			ardahttp.WriteEnvelopeUnpaged(w, r, items)
		case http.MethodPost:
			var in repository.CounterpartyAccount
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				writeInvalidBodyCP(w, r)
				return
			}
			in.CounterpartyID = id
			created, err := h.svc.UpsertAccount(r.Context(), tenantID, &in)
			if err != nil {
				ardahttp.WriteServiceError(w, r, err)
				return
			}
			ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
		default:
			ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
		}
		return
	}
	switch r.Method {
	case http.MethodPut:
		var in repository.Counterparty
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeInvalidBodyCP(w, r)
			return
		}
		created, err := h.svc.UpdateByID(r.Context(), tenantID, id, r.Header.Get("X-User-Id"), &in)
		if err != nil {
			ardahttp.WriteServiceError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, created)
	case http.MethodDelete:
		if err := h.svc.SetActive(r.Context(), tenantID, id, false); err != nil {
			ardahttp.WriteServiceError(w, r, err)
			return
		}
		ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]bool{"ok": true})
	default:
		ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
	}
}

func writeForbiddenCP(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
}

func writeInvalidBodyCP(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
}
