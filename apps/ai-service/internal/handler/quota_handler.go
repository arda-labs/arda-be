package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type QuotasResponseDTO struct {
	WebhookURL        string `json:"webhookUrl"`
	MonthlyTokenLimit int64  `json:"monthlyTokenLimit"`
	TokensUsed        int64  `json:"tokensUsed"`
	PeriodStart       string `json:"periodStart"`
}

type UpdateQuotasRequestDTO struct {
	WebhookURL        string `json:"webhookUrl"`
	MonthlyTokenLimit int64  `json:"monthlyTokenLimit"`
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

	settings, err := quotaStore.GetQuotaSettings(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_fetch_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": QuotasResponseDTO{
			WebhookURL:        settings.WebhookURL,
			MonthlyTokenLimit: settings.MonthlyTokenLimit,
			TokensUsed:        settings.TokensUsed,
			PeriodStart:       settings.PeriodStart,
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

	if err := quotaStore.SaveQuotaSettings(r.Context(), repository.QuotaSettings{
		TenantID:          scope.TenantID,
		WebhookURL:        strings.TrimSpace(req.WebhookURL),
		MonthlyTokenLimit: req.MonthlyTokenLimit,
	}); err != nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_save_failed")
		return
	}

	fresh, err := quotaStore.GetQuotaSettings(r.Context(), scope.TenantID)
	if err != nil || fresh == nil {
		problem(w, http.StatusInternalServerError, "ai.quotas_fetch_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": QuotasResponseDTO{
			WebhookURL:        fresh.WebhookURL,
			MonthlyTokenLimit: fresh.MonthlyTokenLimit,
			TokensUsed:        fresh.TokensUsed,
			PeriodStart:       fresh.PeriodStart,
		},
	})
}
