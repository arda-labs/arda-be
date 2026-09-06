package handler

import (
	"net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
)

// CollectionHandler exposes the LNM.301.02 receipt flow over HTTP.
type CollectionHandler struct {
	svc *service.CollectionService
}

func NewCollectionHandler(svc *service.CollectionService) *CollectionHandler {
	return &CollectionHandler{svc: svc}
}

// ListCollections handles GET /api/loan/collections.
func (h *CollectionHandler) ListCollections(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.svc.List(r.Context(), tenantID, r.URL.Query().Get("status"), r.URL.Query().Get("contract_code"))
	writeResult(w, r, items, err)
}

// CreateCollection handles POST /api/loan/collections.
func (h *CollectionHandler) CreateCollection(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	var req domain.Collection
	if !decodeBody(w, r, &req) {
		return
	}
	created, err := h.svc.Create(r.Context(), tenantID, actorOf(r), &req)
	writeResult(w, r, created, err)
}

// SubmitCollection handles POST /api/loan/collections/{id}/submit.
func (h *CollectionHandler) SubmitCollection(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	item, err := h.svc.Submit(r.Context(), tenantID, actorOf(r), r.PathValue("id"))
	writeResult(w, r, item, err)
}
