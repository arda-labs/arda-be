package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type DepartmentBudgetDTO struct {
	Department   string  `json:"department"`
	MonthlyLimit float64 `json:"monthlyLimit"`
	Spent        float64 `json:"spent"`
	RPMLimit     int     `json:"rpmLimit"`
}

type DepartmentBudgetInputDTO struct {
	Department   string  `json:"department"`
	MonthlyLimit float64 `json:"monthlyLimit"`
	RPMLimit     int     `json:"rpmLimit"`
}

type QuotasResponseDTO struct {
	Budgets           []DepartmentBudgetDTO `json:"budgets"`
	WebhookURL        string                `json:"webhookUrl"`
	MonthlyTokenLimit int64                 `json:"monthlyTokenLimit"`
	TokensUsed        int64                 `json:"tokensUsed"`
	PeriodStart       string                `json:"periodStart"`
}

type UpdateQuotasRequestDTO struct {
	Budgets           []DepartmentBudgetInputDTO `json:"budgets"`
	WebhookURL        string                     `json:"webhookUrl"`
	MonthlyTokenLimit int64                      `json:"monthlyTokenLimit"`
}

func handleGetQuotas(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	quotaStore, ok := store.(repository.QuotaStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	budgets, err := quotaStore.ListDepartmentBudgets(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_fetch_failed")
		return
	}

	settings, err := quotaStore.GetQuotaSettings(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_fetch_failed")
		return
	}

	budgetDTOs := make([]DepartmentBudgetDTO, 0, len(budgets))
	for _, b := range budgets {
		budgetDTOs = append(budgetDTOs, DepartmentBudgetDTO{
			Department:   b.Department,
			MonthlyLimit: b.MonthlyLimit,
			RPMLimit:     b.RPMLimit,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": QuotasResponseDTO{Budgets: budgetDTOs, WebhookURL: settings.WebhookURL,
			MonthlyTokenLimit: settings.MonthlyTokenLimit, TokensUsed: settings.TokensUsed, PeriodStart: settings.PeriodStart},
	})
}

func handleUpdateQuotas(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	quotaStore, ok := store.(repository.QuotaStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	var req UpdateQuotasRequestDTO
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}

	budgets := make([]repository.DepartmentBudget, 0, len(req.Budgets))
	for _, b := range req.Budgets {
		budgets = append(budgets, repository.DepartmentBudget{
			TenantID:     scope.TenantID,
			Department:   strings.TrimSpace(b.Department),
			MonthlyLimit: b.MonthlyLimit,
			RPMLimit:     b.RPMLimit,
		})
	}

	if err := quotaStore.SaveDepartmentBudgets(r.Context(), scope.TenantID, budgets); err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_save_failed")
		return
	}

	if err := quotaStore.SaveQuotaSettings(r.Context(), repository.QuotaSettings{
		TenantID: scope.TenantID, WebhookURL: strings.TrimSpace(req.WebhookURL), MonthlyTokenLimit: req.MonthlyTokenLimit,
	}); err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_save_failed")
		return
	}

	responseBudgets := make([]DepartmentBudgetDTO, 0, len(req.Budgets))
	savedBudgets, err := quotaStore.ListDepartmentBudgets(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_fetch_failed")
		return
	}
	for _, b := range savedBudgets {
		responseBudgets = append(responseBudgets, DepartmentBudgetDTO{Department: b.Department, MonthlyLimit: b.MonthlyLimit, Spent: b.Spent, RPMLimit: b.RPMLimit})
	}
	fresh, err := quotaStore.GetQuotaSettings(r.Context(), scope.TenantID)
	if err != nil || fresh == nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_fetch_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result":  QuotasResponseDTO{Budgets: responseBudgets, WebhookURL: req.WebhookURL, MonthlyTokenLimit: req.MonthlyTokenLimit, TokensUsed: fresh.TokensUsed, PeriodStart: fresh.PeriodStart},
	})
}
