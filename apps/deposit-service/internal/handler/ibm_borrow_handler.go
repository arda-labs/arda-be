package handler

import (
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"

	"net/http"

	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// Interbank borrowing (tiền vay TCTD khác) HTTP surface — the mirror of the IBM
// deposit handlers, mounted on the same DepositHandler.

// ListBorrows handles GET /api/deposit/borrows.
func (h *DepositHandler) ListBorrows(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	scope := orgScopeFrom(r)
	items, err := h.ibm.ListBorrows(r.Context(), tenantID, scope.OrgIDs, r.URL.Query().Get("status"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}

// CreateBorrow handles POST /api/deposit/borrows (stages PENDING_APPROVAL).
func (h *DepositHandler) CreateBorrow(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in repository.InterbankBorrow
	if !decodeDepositBody(w, r, &in) {
		return
	}
	in.OrgCode = orgScopeFrom(r).ActiveOrgID
	created, err := h.ibm.SubmitBorrow(r.Context(), tenantID, r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}

// GetBorrowDetail handles GET /api/deposit/borrows/{id}.
func (h *DepositHandler) GetBorrowDetail(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	borrow, movements, err := h.ibm.GetBorrowDetail(r.Context(), tenantID, r.PathValue("id"))
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{"borrow": borrow, "movements": movements})
}

// DecideBorrow handles POST /api/deposit/borrows/{id}/decision.
func (h *DepositHandler) DecideBorrow(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in struct {
		Decision    string `json:"decision"`
		DataVersion string `json:"data_version"`
	}
	if !decodeDepositBody(w, r, &in) {
		return
	}
	updated, err := h.ibm.DecideBorrow(r.Context(), tenantID, r.PathValue("id"),
		in.Decision, r.Header.Get("X-User-Id"), in.DataVersion)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, updated)
}

// SubmitBorrowMovement handles POST /api/deposit/borrows/{id}/movements.
func (h *DepositHandler) SubmitBorrowMovement(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "tenant scope is required"))
		return
	}
	var in repository.IBMBorrowMovement
	if !decodeDepositBody(w, r, &in) {
		return
	}
	created, err := h.ibm.SubmitBorrowMovement(r.Context(), tenantID, r.PathValue("id"),
		r.Header.Get("X-User-Id"), &in)
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusCreated, created)
}
