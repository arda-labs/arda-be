package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// DepositHandler exposes the deposit HTTP surface (P2.1).
type DepositHandler struct {
	svc *service.SettlementService
}

func NewDepositHandler(svc *service.SettlementService) *DepositHandler {
	return &DepositHandler{svc: svc}
}

type orgScope struct {
	ActiveOrgID string
	OrgIDs      []string
}

func orgScopeFrom(r *http.Request) orgScope {
	var ids []string
	if raw := strings.TrimSpace(r.Header.Get("X-User-Org-Ids")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				ids = append(ids, part)
			}
		}
	}
	return orgScope{ActiveOrgID: strings.TrimSpace(r.Header.Get("X-Org-Id")), OrgIDs: ids}
}

func (s orgScope) listFilter() []string {
	if s.ActiveOrgID != "" && s.allows(s.ActiveOrgID) {
		return []string{s.ActiveOrgID}
	}
	return s.OrgIDs
}

func (s orgScope) active() string {
	if s.ActiveOrgID != "" && s.allows(s.ActiveOrgID) {
		return s.ActiveOrgID
	}
	if s.ActiveOrgID == "" && len(s.OrgIDs) == 1 {
		return s.OrgIDs[0]
	}
	return s.ActiveOrgID
}

func (s orgScope) allows(orgID string) bool {
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

// ListProducts handles GET /api/deposit/products.
func (h *DepositHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	// svc.ListProducts via repo passthrough — exposed for the deposit remote.
	items, err := h.svc.ListProducts(r.Context(), tenantID)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// OpenSavings handles POST /api/deposit/savings/open.
func (h *DepositHandler) OpenSavings(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in service.OpenSavingsInput
	if !decodeDepositBody(w, r, &in) {
		return
	}
	scope := orgScopeFrom(r)
	in.Actor = r.Header.Get("X-User-Id")
	in.OrgCode = scope.active()
	created, err := h.svc.Open(r.Context(), tenantID, &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// SettleSavings handles POST /api/deposit/savings/{code}/settle.
func (h *DepositHandler) SettleSavings(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	savings, err := h.svc.Settle(r.Context(), tenantID, r.PathValue("code"), r.Header.Get("X-User-Id"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, savings)
}

// ListSavings handles GET /api/deposit/savings.
func (h *DepositHandler) ListSavings(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	scope := orgScopeFrom(r)
	items, err := h.svc.ListSavings(r.Context(), tenantID, scope.listFilter(),
		r.URL.Query().Get("status"), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

func decodeDepositBody(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := jsonDecodeDeposit(r, target); err != nil {
		ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeInvalidJSON, "invalid body"))
		return false
	}
	return true
}

// ListInterbankDeposits handles GET /api/deposit/interbank.
func (h *DepositHandler) ListInterbankDeposits(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	scope := orgScopeFrom(r)
	items, err := h.svc.ListInterbank(r.Context(), tenantID, scope.listFilter(), r.URL.Query().Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}
