package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

// agentSettingsDTO is the act-mode configuration exposed to the AI Settings UI.
type agentSettingsDTO struct {
	ActModeEnabled bool   `json:"act_mode_enabled"`
	ActModeMaxRisk string `json:"act_mode_max_risk"`
}

// handleAgentSettings serves GET/PUT /api/ai/settings/agent: the per-tenant
// Ask/Act configuration. Writes are admin-gated by policy.yaml (ai-settings-write);
// the value only ever enables auto-execution for low/medium risk.
func handleAgentSettings(w http.ResponseWriter, r *http.Request, store runStore) {
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	settingsStore, ok := store.(repository.AgentSettingsStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	switch r.Method {
	case http.MethodGet:
		settings, err := settingsStore.GetAgentSettings(r.Context(), scope.TenantID)
		if err != nil || settings == nil {
			problem(w, http.StatusInternalServerError, "ai.agent_settings_fetch_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result":  agentSettingsDTO{ActModeEnabled: settings.ActModeEnabled, ActModeMaxRisk: settings.ActModeMaxRisk},
		})
	case http.MethodPut, http.MethodPost:
		var req agentSettingsDTO
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
			problem(w, http.StatusBadRequest, "ai.invalid_request_body")
			return
		}
		risk := strings.ToLower(strings.TrimSpace(req.ActModeMaxRisk))
		if risk == "" {
			risk = "medium"
		}
		if risk != "low" && risk != "medium" {
			problem(w, http.StatusBadRequest, "ai.agent_settings_invalid_risk")
			return
		}
		if err := settingsStore.UpsertAgentSettings(r.Context(), scope.TenantID, req.ActModeEnabled, risk); err != nil {
			problem(w, http.StatusInternalServerError, "ai.agent_settings_save_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result":  agentSettingsDTO{ActModeEnabled: req.ActModeEnabled, ActModeMaxRisk: risk},
		})
	default:
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
	}
}
