package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/service"
)

// FinanceHandler exposes finance API endpoints.
type FinanceHandler struct {
	svc *service.LedgerService
}

func NewFinanceHandler(svc *service.LedgerService) *FinanceHandler {
	return &FinanceHandler{svc: svc}
}

// ── Accounts ──

func (h *FinanceHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	accounts, err := h.svc.ListAccounts(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

func (h *FinanceHandler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code          string `json:"code"`
		Name          string `json:"name"`
		Type          string `json:"type"`
		NormalBalance string `json:"normalBalance"`
		Currency      string `json:"currency"`
		ParentID      string `json:"parentId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" || req.Name == "" || req.Type == "" || req.NormalBalance == "" {
		respondError(w, http.StatusBadRequest, "code, name, type, normalBalance required")
		return
	}

	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}

	acct := &domain.Account{
		TenantID:      tenantID,
		Code:          req.Code,
		Name:          req.Name,
		Type:          domain.AccountType(req.Type),
		NormalBalance: domain.NormalBalance(req.NormalBalance),
		Currency:      req.Currency,
		IsActive:      true,
		ParentID:      req.ParentID,
	}

	created, err := h.svc.CreateAccount(r.Context(), acct)
	if err != nil {
		respondError(w, http.StatusConflict, err.Error())
		return
	}
	respondJSON(w, http.StatusCreated, created)
}

func (h *FinanceHandler) GetAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}
	acct, err := h.svc.GetAccount(r.Context(), id)
	if err != nil || acct == nil {
		respondError(w, http.StatusNotFound, "account not found")
		return
	}

	balance, _ := h.svc.GetAccountBalance(r.Context(), id)

	respondJSON(w, http.StatusOK, map[string]any{
		"account": acct,
		"balance": balance,
	})
}

func (h *FinanceHandler) GetAccountBalance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}
	balance, err := h.svc.GetAccountBalance(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "account not found")
		return
	}
	respondJSON(w, http.StatusOK, balance)
}

// ── Transactions ──

func (h *FinanceHandler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdempotencyKey string `json:"idempotencyKey"`
		TxnType        string `json:"txnType"`
		TxnDate        string `json:"txnDate"`
		Description    string `json:"description"`
		SourceRef      string `json:"sourceRef"`
		Entries        []struct {
			AccountID string `json:"accountId"`
			Type      string `json:"type"`   // DEBIT or CREDIT
			Amount    string `json:"amount"`
			Currency  string `json:"currency"`
		} `json:"entries"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.TxnType == "" || len(req.Entries) == 0 {
		respondError(w, http.StatusBadRequest, "txnType and entries required")
		return
	}

	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		userID = r.Header.Get("X-User-Subject")
	}

	entries := make([]domain.LedgerEntry, len(req.Entries))
	for i, e := range req.Entries {
		if e.Currency == "" {
			e.Currency = "VND"
		}
		entries[i] = domain.LedgerEntry{
			AccountID: e.AccountID,
			EntryType: domain.EntryType(e.Type),
			Amount:    e.Amount,
			Currency:  e.Currency,
		}
	}

	txn := &domain.Transaction{
		TenantID:       tenantID,
		IdempotencyKey: req.IdempotencyKey,
		TxnType:        req.TxnType,
		TxnDate:        req.TxnDate,
		Description:    req.Description,
		SourceRef:      req.SourceRef,
		CreatedBy:      userID,
		Entries:        entries,
	}

	result, err := h.svc.PostTransaction(r.Context(), txn)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, result)
}

func (h *FinanceHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}
	txn, err := h.svc.GetTransaction(r.Context(), id)
	if err != nil || txn == nil {
		respondError(w, http.StatusNotFound, "transaction not found")
		return
	}
	respondJSON(w, http.StatusOK, txn)
}

func (h *FinanceHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	status := r.URL.Query().Get("status")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size < 1 || size > 100 {
		size = 20
	}
	var from, to time.Time
	if f := r.URL.Query().Get("from"); f != "" {
		from, _ = time.Parse(time.RFC3339, f)
	}
	if t := r.URL.Query().Get("to"); t != "" {
		to, _ = time.Parse(time.RFC3339, t)
	}

	txns, total, err := h.svc.ListTransactions(r.Context(), tenantID, status, from, to, page, size)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	totalPages := (total + size - 1) / size
	respondJSON(w, http.StatusOK, map[string]any{
		"transactions": txns, "total": total, "page": page, "size": size, "totalPages": totalPages,
	})
}

func (h *FinanceHandler) ReverseTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, http.StatusBadRequest, "missing id")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Reason == "" {
		req.Reason = "manual reversal"
	}

	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		userID = "system"
	}

	result, err := h.svc.ReverseTransaction(r.Context(), id, req.Reason, userID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, result)
}

// ── Trial Balance ──

func (h *FinanceHandler) TrialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	accounts, err := h.svc.ListAccounts(r.Context(), tenantID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type tbEntry struct {
		Account *domain.Account   `json:"account"`
		Balance *domain.AccountBalance `json:"balance"`
	}

	var entries []tbEntry
	for _, a := range accounts {
		b, _ := h.svc.GetAccountBalance(r.Context(), a.ID)
		entries = append(entries, tbEntry{Account: &a, Balance: b})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"tenantId": tenantID,
		"entries":  entries,
	})
}

// ── Shared ──

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
