package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/decision"
	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type decisionEvaluator interface {
	Evaluate(context.Context, decision.Settings, string) (*decision.Result, error)
}

type decisionSettingsDTO struct {
	Enabled       bool    `json:"enabled"`
	ModelID       string  `json:"model_id"`
	MinConfidence float64 `json:"min_confidence"`
	HasAPIKey     bool    `json:"has_api_key"`
	Provider      string  `json:"provider"`
	Purpose       string  `json:"purpose"`
}

type decisionSettingsRequest struct {
	Enabled       bool    `json:"enabled"`
	ModelID       string  `json:"model_id"`
	MinConfidence float64 `json:"min_confidence"`
	// Omitted preserves the saved credential; empty explicitly clears it.
	APIKey *string `json:"api_key,omitempty"`
}

type decisionTestResponse struct {
	Success    bool    `json:"success"`
	LatencyMS  int64   `json:"latency_ms"`
	ModelID    string  `json:"model_id,omitempty"`
	Skill      string  `json:"skill,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Error      string  `json:"error,omitempty"`
}

func decisionDTO(s decision.Settings) decisionSettingsDTO {
	return decisionSettingsDTO{s.Enabled, s.ModelID, s.MinConfidence, s.APIKey != "", "opencode-zen", "decision"}
}

func decisionClient(options RouterOptions) decisionEvaluator {
	if options.DecisionEvaluator != nil {
		return options.DecisionEvaluator
	}
	return decision.NewClient(nil)
}

func handleDecisionSettings(w http.ResponseWriter, r *http.Request, store runStore, options RouterOptions) {
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	probe := r.URL.Path == "/api/ai/settings/decision/test"
	if (probe && r.Method != http.MethodPost) || (!probe && r.Method != http.MethodGet && r.Method != http.MethodPut) {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	settingsStore, ok := store.(repository.DecisionSettingsStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	saved, err := settingsStore.GetDecisionSettings(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "ai.decision_settings_unavailable")
		return
	}
	if r.Method == http.MethodGet {
		writeResultEnvelope(w, decisionDTO(saved))
		return
	}
	var input decisionSettingsRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || ensureEOF(decoder) != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	next := decision.Settings{Enabled: input.Enabled, ModelID: strings.TrimSpace(input.ModelID), MinConfidence: input.MinConfidence, APIKey: saved.APIKey}
	if input.APIKey != nil {
		key := strings.TrimSpace(*input.APIKey)
		if len(key) > 4096 || strings.ContainsAny(key, "\r\n") || strings.HasPrefix(key, "enc:v1:") || isMaskedSecret(key) {
			problem(w, http.StatusBadRequest, "ai.decision_settings_invalid")
			return
		}
		next.APIKey = key
		input.APIKey = &key
	}
	if !next.Valid() || ((next.Enabled || probe) && next.APIKey == "") {
		problem(w, http.StatusBadRequest, "ai.decision_settings_invalid")
		return
	}
	if (next.Enabled || probe) && !baseURLAllowed(options.ModelBaseURLAllowlist, decision.BaseURL) {
		problem(w, http.StatusBadRequest, "ai.base_url_not_allowed")
		return
	}
	if probe {
		started := time.Now()
		result, err := decisionClient(options).Evaluate(r.Context(), next, "Latest user request: Phân tích dư nợ theo nhóm nợ kỳ 2026-08")
		out := decisionTestResponse{LatencyMS: time.Since(started).Milliseconds()}
		if err != nil {
			out.Error = "provider_unavailable"
		} else {
			out.Success = true
			out.ModelID = result.Model
			out.Skill = result.Skill(next.MinConfidence)
			out.Confidence = decisionConfidence(result)
		}
		writeResultEnvelope(w, out)
		return
	}
	if err := settingsStore.SaveDecisionSettings(r.Context(), scope.TenantID, next, input.APIKey); err != nil {
		problem(w, http.StatusServiceUnavailable, "ai.decision_settings_unavailable")
		return
	}
	writeResultEnvelope(w, decisionDTO(next))
}

func decisionConfidence(result *decision.Result) float64 {
	if result == nil || result.Answers["skill"].Confidence == nil {
		return 0
	}
	return *result.Answers["skill"].Confidence
}
