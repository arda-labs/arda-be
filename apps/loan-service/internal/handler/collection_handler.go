package handler

import (
	"net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// CollectionHandler exposes the LNM.301.02 receipt flow over HTTP.
type CollectionHandler struct {
	svc *service.CollectionService
}

func NewCollectionHandler(svc *service.CollectionService) *CollectionHandler {
	return &CollectionHandler{svc: svc}
}

// collectionListSpec is the ParseListRequest contract for the collections
// ledger: q (agreement/contract code ILIKE), status + contract_code filters
// and a SQL sort whitelist. SQL paging — no full-table slice.
var collectionListSpec = ardahttp.ListSpec{
	DefaultPerPage: 20,
	MaxPerPage:     ardahttp.MaxPerPage,
	SortFields:     []string{"agreement_code", "contract_code", "collection_date", "created_at"},
}

// ListCollections handles GET /api/loan/collections.
func (h *CollectionHandler) ListCollections(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), collectionListSpec)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, ardaerrors.CodeInvalidInput, err.Error())
		return
	}
	items, total, err := h.svc.List(r.Context(), tenantID, orgScopeFromRequest(r).ListFilter(),
		r.URL.Query().Get("status"), r.URL.Query().Get("contract_code"),
		listReq.Q, listReq.Sort, listReq.Order, listReq.Page, listReq.PerPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(listReq.Page, listReq.PerPage, total, items))
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
	req.OrgCode = orgScopeFromRequest(r).ActiveOrg()
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
