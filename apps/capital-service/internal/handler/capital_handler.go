package handler

import (
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

// CapitalHandler exposes the CFM HTTP surface (P2.2).
type CapitalHandler struct {
	svc *service.CapitalService
}

func NewCapitalHandler(svc *service.CapitalService) *CapitalHandler {
	return &CapitalHandler{svc: svc}
}

// ListFundTypes handles GET /api/capital/fund-types.
func (h *CapitalHandler) ListFundTypes(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	items, err := h.svc.ListFundTypes(r.Context(), tenantID)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// ListContracts handles GET /api/capital/contracts.
func (h *CapitalHandler) ListContracts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		writeForbidden(w, r)
		return
	}
	items, err := h.svc.ListContracts(r.Context(), tenantID, orgScopeFromCap(r).listFilter(), r.URL.Query().Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// CreateContract handles POST /api/capital/contracts.
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
	created, err := h.svc.CreateContract(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// RecordMovement handles POST /api/capital/contracts/{id}/movements.
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

var _ = strings.TrimSpace
