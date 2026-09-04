package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type RoutingRulesDTO struct {
	FastModel         string `json:"fastModel"`
	CodeModel         string `json:"codeModel"`
	SensitiveModel    string `json:"sensitiveModel"`
	PrimaryProvider   string `json:"primaryProvider"`
	SecondaryProvider string `json:"secondaryProvider"`
	FailoverProvider  string `json:"failoverProvider"`
}

func handleGetRouting(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	routingStore, ok := store.(repository.RoutingStore)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result": RoutingRulesDTO{
				FastModel:         "gemini-2.5-flash",
				CodeModel:         "claude-3.5-sonnet",
				SensitiveModel:    "qwen2.5:7b-instruct-q4_K_M",
				PrimaryProvider:   "gemini",
				SecondaryProvider: "openai",
				FailoverProvider:  "ollama",
			},
		})
		return
	}
	rules, err := routingStore.GetRoutingRules(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.routing_fetch_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": RoutingRulesDTO{
			FastModel:         rules.FastModel,
			CodeModel:         rules.CodeModel,
			SensitiveModel:    rules.SensitiveModel,
			PrimaryProvider:   rules.PrimaryProvider,
			SecondaryProvider: rules.SecondaryProvider,
			FailoverProvider:  rules.FailoverProvider,
		},
	})
}

func handleUpdateRouting(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	routingStore, ok := store.(repository.RoutingStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	var req RoutingRulesDTO
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}

	updated, err := routingStore.SaveRoutingRules(r.Context(), repository.TenantRoutingRules{
		TenantID:          scope.TenantID,
		FastModel:         strings.TrimSpace(req.FastModel),
		CodeModel:         strings.TrimSpace(req.CodeModel),
		SensitiveModel:    strings.TrimSpace(req.SensitiveModel),
		PrimaryProvider:   strings.TrimSpace(req.PrimaryProvider),
		SecondaryProvider: strings.TrimSpace(req.SecondaryProvider),
		FailoverProvider:  strings.TrimSpace(req.FailoverProvider),
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.routing_save_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": RoutingRulesDTO{
			FastModel:         updated.FastModel,
			CodeModel:         updated.CodeModel,
			SensitiveModel:    updated.SensitiveModel,
			PrimaryProvider:   updated.PrimaryProvider,
			SecondaryProvider: updated.SecondaryProvider,
			FailoverProvider:  updated.FailoverProvider,
		},
	})
}
