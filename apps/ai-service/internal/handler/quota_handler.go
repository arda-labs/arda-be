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

type QuotasResponseDTO struct {
	Budgets    []DepartmentBudgetDTO `json:"budgets"`
	WebhookURL string                `json:"webhookUrl"`
}

type UpdateQuotasRequestDTO struct {
	Budgets    []DepartmentBudgetDTO `json:"budgets"`
	WebhookURL string                `json:"webhookUrl"`
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
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result": QuotasResponseDTO{
				Budgets: []DepartmentBudgetDTO{
					{Department: "Tech & DevOps", MonthlyLimit: 300, Spent: 118.2, RPMLimit: 120},
					{Department: "Sales & Marketing", MonthlyLimit: 150, Spent: 42.5, RPMLimit: 60},
					{Department: "HR & Internal Ops", MonthlyLimit: 80, Spent: 15.4, RPMLimit: 30},
					{Department: "Finance & Accounting", MonthlyLimit: 100, Spent: 22.1, RPMLimit: 40},
				},
				WebhookURL: "https://hooks.slack.com/services/T00/B00/XXXX",
			},
		})
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
			Spent:        b.Spent,
			RPMLimit:     b.RPMLimit,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": QuotasResponseDTO{
			Budgets:    budgetDTOs,
			WebhookURL: settings.WebhookURL,
		},
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
			Spent:        b.Spent,
			RPMLimit:     b.RPMLimit,
		})
	}

	if err := quotaStore.SaveDepartmentBudgets(r.Context(), scope.TenantID, budgets); err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_save_failed")
		return
	}

	if err := quotaStore.SaveQuotaSettings(r.Context(), repository.QuotaSettings{
		TenantID:   scope.TenantID,
		WebhookURL: strings.TrimSpace(req.WebhookURL),
	}); err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_save_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": QuotasResponseDTO{
			Budgets:    req.Budgets,
			WebhookURL: req.WebhookURL,
		},
	})
}
