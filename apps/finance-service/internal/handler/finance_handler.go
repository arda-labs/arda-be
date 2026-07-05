package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// FinanceHandler exposes finance API endpoints.
type FinanceHandler struct {
	svc       *service.LedgerService
	ops       *service.FinanceOperationService
	configSvc *service.AccountingConfigService
}

func NewFinanceHandler(svc *service.LedgerService, ops *service.FinanceOperationService, configSvc *service.AccountingConfigService) *FinanceHandler {
	return &FinanceHandler{svc: svc, ops: ops, configSvc: configSvc}
}

// ── Accounts ──

func (h *FinanceHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	accounts, err := h.svc.ListAccounts(r.Context(), tenantID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"accounts": accounts})
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
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Code == "" || req.Name == "" || req.Type == "" || req.NormalBalance == "" {
		respondError(w, r, http.StatusBadRequest, "code, name, type, normalBalance required")
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
		respondError(w, r, http.StatusConflict, err.Error())
		return
	}
	respondJSON(w, r, http.StatusCreated, created)
}

func (h *FinanceHandler) GetAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing id")
		return
	}
	acct, err := h.svc.GetAccount(r.Context(), id)
	if err != nil || acct == nil {
		respondError(w, r, http.StatusNotFound, "account not found")
		return
	}

	balance, _ := h.svc.GetAccountBalance(r.Context(), id)

	respondJSON(w, r, http.StatusOK, map[string]any{
		"account": acct,
		"balance": balance,
	})
}

func (h *FinanceHandler) GetAccountBalance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing id")
		return
	}
	balance, err := h.svc.GetAccountBalance(r.Context(), id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "account not found")
		return
	}
	respondJSON(w, r, http.StatusOK, balance)
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
			Type      string `json:"type"` // DEBIT or CREDIT
			Amount    string `json:"amount"`
			Currency  string `json:"currency"`
		} `json:"entries"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	if req.TxnType == "" || len(req.Entries) == 0 {
		respondError(w, r, http.StatusBadRequest, "txnType and entries required")
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
	if txn.IdempotencyKey == "" {
		txn.IdempotencyKey = r.Header.Get("X-Idempotency-Key")
	}

	result, err := h.svc.PostTransaction(r.Context(), txn)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusCreated, result)
}

func (h *FinanceHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing id")
		return
	}
	txn, err := h.svc.GetTransaction(r.Context(), id)
	if err != nil || txn == nil {
		respondError(w, r, http.StatusNotFound, "transaction not found")
		return
	}
	respondJSON(w, r, http.StatusOK, txn)
}

func (h *FinanceHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	status := r.URL.Query().Get("status")
	listQuery := ardahttp.ParseListQuery(r.URL.Query())
	page := listQuery.Page
	perPage := listQuery.PerPage
	var from, to time.Time
	if f := r.URL.Query().Get("from"); f != "" {
		from, _ = time.Parse(time.RFC3339, f)
	}
	if t := r.URL.Query().Get("to"); t != "" {
		to, _ = time.Parse(time.RFC3339, t)
	}

	txns, total, err := h.svc.ListTransactions(r.Context(), tenantID, status, from, to, page, perPage)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondPaged(w, r, txns, total, page, perPage)
}

func (h *FinanceHandler) SearchTransactions(w http.ResponseWriter, r *http.Request) {
	page, size := pageSizeFrom(r)
	from, to := dateRangeFrom(r)
	txns, total, err := h.ops.Search(r.Context(), repository.TransactionSearchFilter{
		TenantID:  tenantIDFrom(r),
		Keyword:   strings.TrimSpace(r.URL.Query().Get("keyword")),
		Direction: r.URL.Query().Get("direction"),
		CaseType:  r.URL.Query().Get("case_type"),
		Status:    r.URL.Query().Get("status"),
		TxnType:   r.URL.Query().Get("txn_type"),
		From:      from,
		To:        to,
		Page:      page,
		Size:      size,
	})
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondPaged(w, r, txns, total, page, size)
}

func (h *FinanceHandler) ListIncomingTransactions(w http.ResponseWriter, r *http.Request) {
	h.listOperationTransactions(w, r, string(domain.TxnDirectionIncoming))
}

func (h *FinanceHandler) ListOutgoingTransactions(w http.ResponseWriter, r *http.Request) {
	h.listOperationTransactions(w, r, string(domain.TxnDirectionOutgoing))
}

func (h *FinanceHandler) CreateIncomingTransaction(w http.ResponseWriter, r *http.Request) {
	h.createOperationTransaction(w, r, h.ops.CreateIncoming)
}

func (h *FinanceHandler) CreateOutgoingTransaction(w http.ResponseWriter, r *http.Request) {
	h.createOperationTransaction(w, r, h.ops.CreateOutgoing)
}

func (h *FinanceHandler) GetOperationTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing id")
		return
	}
	txn, err := h.ops.Get(r.Context(), id)
	if err != nil || txn == nil {
		respondError(w, r, http.StatusNotFound, "transaction not found")
		return
	}
	respondJSON(w, r, http.StatusOK, txn)
}

func (h *FinanceHandler) listOperationTransactions(w http.ResponseWriter, r *http.Request, direction string) {
	page, size := pageSizeFrom(r)
	from, to := dateRangeFrom(r)
	txns, total, err := h.ops.Search(r.Context(), repository.TransactionSearchFilter{
		TenantID:  tenantIDFrom(r),
		Keyword:   strings.TrimSpace(r.URL.Query().Get("keyword")),
		Direction: direction,
		Status:    r.URL.Query().Get("status"),
		TxnType:   r.URL.Query().Get("txn_type"),
		From:      from,
		To:        to,
		Page:      page,
		Size:      size,
	})
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondPaged(w, r, txns, total, page, size)
}

func (h *FinanceHandler) createOperationTransaction(w http.ResponseWriter, r *http.Request, create func(context.Context, string, service.OperationCreateRequest) (*domain.Transaction, error)) {
	var req struct {
		IdempotencyKey      string `json:"idempotencyKey"`
		TxnType             string `json:"txnType"`
		TxnDate             string `json:"txnDate"`
		Amount              string `json:"amount"`
		Currency            string `json:"currency"`
		Description         string `json:"description"`
		SourceRef           string `json:"sourceRef"`
		CounterpartyName    string `json:"counterpartyName"`
		CounterpartyAccount string `json:"counterpartyAccount"`
		Priority            string `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid body")
		return
	}
	key := req.IdempotencyKey
	if key == "" {
		key = r.Header.Get("X-Idempotency-Key")
	}
	txn, err := create(r.Context(), tenantIDFrom(r), service.OperationCreateRequest{
		IdempotencyKey:      key,
		TxnType:             req.TxnType,
		TxnDate:             req.TxnDate,
		Amount:              req.Amount,
		Currency:            req.Currency,
		Description:         req.Description,
		SourceRef:           req.SourceRef,
		CounterpartyName:    req.CounterpartyName,
		CounterpartyAccount: req.CounterpartyAccount,
		Priority:            req.Priority,
		CreatedBy:           userIDFrom(r),
	})
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, r, http.StatusCreated, txn)
}

func (h *FinanceHandler) ReverseTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		respondError(w, r, http.StatusBadRequest, "missing id")
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
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusCreated, result)
}

// ── Trial Balance ──

func (h *FinanceHandler) TrialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		tenantID = "default"
	}
	accounts, err := h.svc.ListAccounts(r.Context(), tenantID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	type tbEntry struct {
		Account *domain.Account        `json:"account"`
		Balance *domain.AccountBalance `json:"balance"`
	}

	var entries []tbEntry
	for _, a := range accounts {
		b, _ := h.svc.GetAccountBalance(r.Context(), a.ID)
		entries = append(entries, tbEntry{Account: &a, Balance: b})
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
		"tenantId": tenantID,
		"entries":  entries,
	})
}

// ── Shared ──

func (h *FinanceHandler) ListProcessConfigs(w http.ResponseWriter, r *http.Request) {
	items, err := h.configSvc.ListProcessConfigs(r.Context(), tenantIDFrom(r))
	respondList(w, r, items, err)
}

func (h *FinanceHandler) ListAccountClassifications(w http.ResponseWriter, r *http.Request) {
	items, err := h.configSvc.ListAccountClassifications(r.Context(), tenantIDFrom(r))
	respondList(w, r, items, err)
}

func (h *FinanceHandler) ListJournalDefinitions(w http.ResponseWriter, r *http.Request) {
	items, err := h.configSvc.ListJournalDefinitions(r.Context(), tenantIDFrom(r))
	respondList(w, r, items, err)
}

func (h *FinanceHandler) ListRegulatoryAccounts(w http.ResponseWriter, r *http.Request) {
	items, err := h.configSvc.ListRegulatoryAccounts(r.Context(), tenantIDFrom(r))
	respondList(w, r, items, err)
}

func (h *FinanceHandler) ListInternalAccounts(w http.ResponseWriter, r *http.Request) {
	items, err := h.configSvc.ListInternalAccounts(r.Context(), tenantIDFrom(r))
	respondList(w, r, items, err)
}

func tenantIDFrom(r *http.Request) string {
	tenantID := r.Header.Get("X-Tenant-Id")
	if tenantID == "" {
		return "default"
	}
	return tenantID
}

func userIDFrom(r *http.Request) string {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		userID = r.Header.Get("X-User-Subject")
	}
	if userID == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return userID
}

func pageSizeFrom(r *http.Request) (int, int) {
	listQuery := ardahttp.ParseListQuery(r.URL.Query())
	return listQuery.Page, listQuery.PerPage
}

func dateRangeFrom(r *http.Request) (time.Time, time.Time) {
	var from, to time.Time
	if f := r.URL.Query().Get("from"); f != "" {
		from, _ = time.Parse(time.RFC3339, f)
	}
	if t := r.URL.Query().Get("to"); t != "" {
		to, _ = time.Parse(time.RFC3339, t)
	}
	return from, to
}
