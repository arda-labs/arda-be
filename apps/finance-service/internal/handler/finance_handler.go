package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// accountListSpec is the public list contract for the account master: q,
// page/per_page, and a sort whitelist kept in sync with the FE list
// definition (code | name | created_at).
var accountListSpec = ardahttp.ListSpec{
	DefaultPerPage: 20,
	MaxPerPage:     ardahttp.MaxPerPage,
	SortFields:     []string{"code", "name", "created_at"},
	AllowAll:       true,
}

// FinanceHandler exposes the finance API: account master, trial balance
// (journal-aggregated) and accounting configuration. Posting lives on the
// gRPC PostingService; legacy transaction/approval endpoints were removed
// in the Phase 0 rebuild.
type FinanceHandler struct {
	accounts  *service.AccountService
	trialBal  *service.TrialBalanceService
	configSvc *service.AccountingConfigService
	cash      *service.CashService
}

func NewFinanceHandler(accounts *service.AccountService, trialBal *service.TrialBalanceService, configSvc *service.AccountingConfigService, cash *service.CashService) *FinanceHandler {
	return &FinanceHandler{accounts: accounts, trialBal: trialBal, configSvc: configSvc, cash: cash}
}

// ListCashPosition handles GET /api/finance/cash-position (VCM aggregate).
func (h *FinanceHandler) ListCashPosition(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	rows, err := h.cash.Position(r.Context(), tenantID)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"rows": rows})
}

// ── Accounts ──

// ListAccounts handles GET /api/finance/accounts. The endpoint accepts the
// standard list contract (ardahttp.ParseListRequest): q ILIKEs code+name,
// sort is whitelisted to code|name|created_at in the repo, paging via
// page/per_page. Write envelope is unchanged (accounts list stays the
// SuccessEnvelope shape for legacy callers).
func (h *FinanceHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	listReq, err := ardahttp.ParseListRequest(r.URL.Query(), accountListSpec)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	accounts, total, err := h.accounts.ListAccountsPaged(r.Context(), tenantID, service.AccountListParams{
		Page:   listReq.Page,
		Size:   listReq.PerPage,
		Search: listReq.Q,
		Sort:   listReq.Sort,
		Order:  listReq.Order,
	})
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"accounts": accounts,
		"page":     listReq.Page,
		"per_page": listReq.PerPage,
		"total":    total,
	})
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

	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
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

	created, err := h.accounts.CreateAccount(r.Context(), acct)
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
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	acct, err := h.accounts.GetAccount(r.Context(), tenantID, id)
	if err != nil || acct == nil {
		respondError(w, r, http.StatusNotFound, "account not found")
		return
	}
	respondJSON(w, r, http.StatusOK, acct)
}

// ── Trial Balance (journal-aggregated; P1a.6) ──

func (h *FinanceHandler) TrialBalance(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	result, err := h.trialBal.TrialBalance(r.Context(), tenantID, r.URL.Query().Get("as_of"))
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, result)
}

// ── Accounting config ──

func (h *FinanceHandler) ListProcessConfigs(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.configSvc.ListProcessConfigs(r.Context(), tenantID)
	respondList(w, r, items, err)
}

func (h *FinanceHandler) ListAccountClassifications(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.configSvc.ListAccountClassifications(r.Context(), tenantID)
	respondList(w, r, items, err)
}

func (h *FinanceHandler) ListJournalDefinitions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.configSvc.ListJournalDefinitions(r.Context(), tenantID)
	respondList(w, r, items, err)
}

func (h *FinanceHandler) ListRegulatoryAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.configSvc.ListRegulatoryAccounts(r.Context(), tenantID)
	respondList(w, r, items, err)
}

func (h *FinanceHandler) ListInternalAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := requireTenantID(w, r)
	if !ok {
		return
	}
	items, err := h.configSvc.ListInternalAccounts(r.Context(), tenantID)
	respondList(w, r, items, err)
}

func tenantIDFrom(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
}

func requireTenantID(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := tenantIDFrom(r)
	if tenantID == "" {
		respondError(w, r, http.StatusForbidden, "tenant scope is required")
		return "", false
	}
	return tenantID, true
}
