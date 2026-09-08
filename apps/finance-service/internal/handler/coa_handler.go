package handler

import (
	"encoding/json"
	"net/http"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// CoaHandler serves the COA v2 definition layer.
type CoaHandler struct {
	svc *service.CoaService
}

func NewCoaHandler(svc *service.CoaService) *CoaHandler {
	return &CoaHandler{svc: svc}
}

func (h *CoaHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListVersions(r.Context(), tenantID)
	respondList(w, r, items, err)
}

func (h *CoaHandler) UpsertVersion(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.CoaVersion
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	item, err := h.svc.UpsertVersion(r.Context(), tenantID, &req)
	respond(w, r, item, err)
}

func (h *CoaHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	// nature narrows the chart to one account nature (D | C | B) — the
	// off-balance flow picks its nature-B accounts with nature=B.
	items, err := h.svc.ListAccounts(r.Context(), tenantID, r.URL.Query().Get("version"), r.URL.Query().Get("nature"))
	respondList(w, r, items, err)
}

func (h *CoaHandler) UpsertAccount(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.CoaAccount
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	item, err := h.svc.UpsertAccount(r.Context(), tenantID, &req)
	respond(w, r, item, err)
}

func (h *CoaHandler) ListClassMaps(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListClassMaps(r.Context(), tenantID, r.URL.Query().Get("classification"), r.URL.Query().Get("version"))
	respondList(w, r, items, err)
}

func (h *CoaHandler) UpsertClassMap(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.AccClassCoaMap
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	item, err := h.svc.UpsertClassMap(r.Context(), tenantID, &req)
	respond(w, r, item, err)
}

// ResolveClassification resolves a classification (+optional debt group /
// currency / date) to the COA account to post against.
func (h *CoaHandler) ResolveClassification(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	item, err := h.svc.ResolveClassification(r.Context(), tenantID,
		q.Get("classification"), q.Get("version"), q.Get("debt_group"), q.Get("currency"), q.Get("on_date"))
	respond(w, r, item, err)
}

func (h *CoaHandler) ListStructures(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.ListStructures(r.Context(), tenantID)
	respondList(w, r, items, err)
}

func (h *CoaHandler) UpsertStructure(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.AccStructure
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	item, err := h.svc.UpsertStructure(r.Context(), tenantID, &req)
	respond(w, r, item, err)
}

func respond(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, data)
}
