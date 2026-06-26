package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
)

// ApprovalHandler exposes approval workflow endpoints.
type ApprovalHandler struct {
	svc *service.ApprovalService
}

func NewApprovalHandler(svc *service.ApprovalService) *ApprovalHandler {
	return &ApprovalHandler{svc: svc}
}

// Create creates an approval request for a transaction.
// POST /api/finance/approvals
func (h *ApprovalHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefID       string `json:"refId"`
		RequestType string `json:"requestType"`
		Amount      string `json:"amount"`
		Note        string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.RefID == "" || req.RequestType == "" {
		respondError(w, http.StatusBadRequest, "refId and requestType required")
		return
	}

	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	makerID := r.Header.Get("X-User-Id")
	if makerID == "" {
		makerID = "system"
	}

	result, err := h.svc.CreateApproval(r.Context(), tenantID, req.RequestType, req.RefID, makerID, req.Note, req.Amount)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, result)
}

// ListPending returns pending approvals for a level.
// GET /api/finance/approvals/pending?level=1
func (h *ApprovalHandler) ListPending(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	level, _ := strconv.Atoi(r.URL.Query().Get("level"))
	if level < 1 {
		level = 1
	}

	requests, err := h.svc.ListPending(r.Context(), tenantID, level)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"approvals": requests})
}

// Approve approves a pending request.
// POST /api/finance/approvals/{id}/approve
func (h *ApprovalHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	checkerID := r.Header.Get("X-User-Id")
	if checkerID == "" {
		checkerID = "system"
	}

	result, err := h.svc.Approve(r.Context(), id, checkerID, req.Note)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// Reject rejects a pending request.
// POST /api/finance/approvals/{id}/reject
func (h *ApprovalHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}

	var req struct {
		Note string `json:"note"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	checkerID := r.Header.Get("X-User-Id")
	if checkerID == "" {
		checkerID = "system"
	}

	result, err := h.svc.Reject(r.Context(), id, checkerID, req.Note)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// Cancel cancels a pending request (maker only).
// POST /api/finance/approvals/{id}/cancel
func (h *ApprovalHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}

	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		userID = "system"
	}

	result, err := h.svc.Cancel(r.Context(), id, userID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// Get returns approval detail.
// GET /api/finance/approvals/{id}
func (h *ApprovalHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}

	result, err := h.svc.GetApproval(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, result)
}
