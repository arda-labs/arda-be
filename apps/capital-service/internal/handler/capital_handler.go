package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/capital-service/internal/repository"
	"github.com/arda-labs/arda/apps/capital-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type capOrgScope struct {
	ActiveOrgID string
	OrgIDs      []string
}

func orgScopeFromCap(r *http.Request) capOrgScope {
	var ids []string
	if raw := strings.TrimSpace(r.Header.Get("X-User-Org-Ids")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				ids = append(ids, part)
			}
		}
	}
	return capOrgScope{ActiveOrgID: strings.TrimSpace(r.Header.Get("X-Org-Id")), OrgIDs: ids}
}

func (s capOrgScope) listFilter() []string {
	if s.ActiveOrgID != "" && s.allows(s.ActiveOrgID) {
		return []string{s.ActiveOrgID}
	}
	return s.OrgIDs
}

func (s capOrgScope) active() string {
	if s.ActiveOrgID != "" && s.allows(s.ActiveOrgID) {
		return s.ActiveOrgID
	}
	if s.ActiveOrgID == "" && len(s.OrgIDs) == 1 {
		return s.OrgIDs[0]
	}
	return s.ActiveOrgID
}

func (s capOrgScope) allows(orgID string) bool {
	if orgID == "" {
		return false
	}
	for _, allowed := range s.OrgIDs {
		if allowed == orgID {
			return true
		}
	}
	return false
}

// CapitalService is the CFM service surface used by the HTTP handler
// (interface for testability).
type CapitalService interface {
	ListFundTypes(ctx context.Context, tenantID string, includeInactive bool) ([]repository.FundType, error)
	CreateFundType(ctx context.Context, tenantID, actor string, in *repository.FundType) (*repository.FundType, error)
	UpdateFundType(ctx context.Context, tenantID, actor string, in *repository.FundType) (*repository.FundType, error)
	DeactivateFundType(ctx context.Context, tenantID, id string) error
	ListProducts(ctx context.Context, tenantID string, includeInactive bool) ([]repository.CapitalProduct, error)
	UpsertProduct(ctx context.Context, tenantID, actor string, in *repository.CapitalProduct) (*repository.CapitalProduct, error)
	ListContracts(ctx context.Context, params repository.ListContractsParams) ([]repository.CapitalContract, int, error)
	GetContractDetail(ctx context.Context, tenantID, id string) (*service.ContractDetail, error)
	CreateContract(ctx context.Context, tenantID, actor string, in *repository.CapitalContract) (*repository.CapitalContract, error)
	SubmitAmendment(ctx context.Context, tenantID, actor, contractID string, payload json.RawMessage, reason string) (*repository.ContractAmendment, error)
	RecordMovement(ctx context.Context, tenantID, actor string, in *repository.CapitalMovement) (*repository.CapitalMovement, error)
}

// CapitalHandler exposes the CFM HTTP surface (P2.2).
type CapitalHandler struct {
	svc CapitalService
}

func NewCapitalHandler(svc CapitalService) *CapitalHandler {
	return &CapitalHandler{svc: svc}
}

// ── Fund types ──

// ListFundTypes handles GET /api/capital/fund-types.
func (h *CapitalHandler) ListFundTypes(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	includeInactive := r.URL.Query().Get("include_inactive") == "true"
	types, err := h.svc.ListFundTypes(r.Context(), tenantID, includeInactive)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, types)
}

// CreateFundType handles POST /api/capital/fund-types.
func (h *CapitalHandler) CreateFundType(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	var in repository.FundType
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.CreateFundType(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// UpdateFundType handles PUT /api/capital/fund-types/{id}.
func (h *CapitalHandler) UpdateFundType(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	var in repository.FundType
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	in.ID = r.PathValue("id")
	updated, err := h.svc.UpdateFundType(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, updated)
}

// DeactivateFundType handles DELETE /api/capital/fund-types/{id}.
func (h *CapitalHandler) DeactivateFundType(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	if err := h.svc.DeactivateFundType(r.Context(), tenantID, r.PathValue("id")); err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]bool{"ok": true})
}

// ── Products ──

// ListProducts handles GET /api/capital/products.
func (h *CapitalHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	includeInactive := r.URL.Query().Get("include_inactive") == "true"
	items, err := h.svc.ListProducts(r.Context(), tenantID, includeInactive)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// UpsertProduct handles POST /api/capital/products (upsert by tenant+code).
func (h *CapitalHandler) UpsertProduct(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	var in repository.CapitalProduct
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.UpsertProduct(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// ── Contracts ──

// ListContracts handles GET /api/capital/contracts.
func (h *CapitalHandler) ListContracts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	list := ardahttp.ParseListQuery(r.URL.Query())
	page := list.Page
	if page < 1 {
		page = 1
	}
	items, total, err := h.svc.ListContracts(r.Context(), repository.ListContractsParams{
		TenantID: tenantID,
		OrgCodes: orgScopeFromCap(r).listFilter(),
		Status:   r.URL.Query().Get("status"),
		Q:        list.Q,
		Sort:     list.Sort,
		Order:    list.Order,
		Page:     (page - 1) * list.PerPage,
		Size:     list.PerPage,
	})
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeList(w, r, http.StatusOK, page, list.PerPage, total, items)
}

// GetContractDetail handles GET /api/capital/contracts/{id}.
func (h *CapitalHandler) GetContractDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	detail, err := h.svc.GetContractDetail(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, detail)
}

// CreateContract handles POST /api/capital/contracts — stages a formation case.
func (h *CapitalHandler) CreateContract(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	var in repository.CapitalContract
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	scope := orgScopeFromCap(r)
	in.OrgCode = scope.active()
	if in.OrgCode == "" {
		in.OrgCode = r.Header.Get("X-Org-Id")
	}
	created, err := h.svc.CreateContract(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// SubmitAmendment handles POST /api/capital/contracts/{id}/amendments.
func (h *CapitalHandler) SubmitAmendment(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	var in struct {
		Payload json.RawMessage `json:"payload"`
		Reason  string          `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	created, err := h.svc.SubmitAmendment(r.Context(), tenantID, r.Header.Get("X-User-Id"),
		r.PathValue("id"), in.Payload, in.Reason)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// RecordMovement handles POST /api/capital/contracts/{id}/movements — stages a
// movement case (RECEIPT/DISBURSEMENT/PAYMENT/SETTLEMENT).
func (h *CapitalHandler) RecordMovement(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	var in repository.CapitalMovement
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return
	}
	in.ContractID = r.PathValue("id")
	in.CreatedBy = r.Header.Get("X-User-Id")
	created, err := h.svc.RecordMovement(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

func writeForbidden(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
}
