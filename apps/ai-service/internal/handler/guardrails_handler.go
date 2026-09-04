package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

type GuardrailsDTO struct {
	PromptInjectionDefense bool    `json:"promptInjectionDefense"`
	PIIMasking             bool    `json:"piiMasking"`
	HallucinationCheck     bool    `json:"hallucinationCheck"`
	ZeroRetention          bool    `json:"zeroRetention"`
	InjectionThreshold     float32 `json:"injectionThreshold"`
}

func handleGetGuardrails(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	gStore, ok := store.(repository.GuardrailsStore)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"result": GuardrailsDTO{
				PromptInjectionDefense: true,
				PIIMasking:             true,
				HallucinationCheck:     true,
				ZeroRetention:          true,
				InjectionThreshold:     0.85,
			},
		})
		return
	}
	g, err := gStore.GetGuardrails(r.Context(), scope.TenantID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.guardrails_fetch_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": GuardrailsDTO{
			PromptInjectionDefense: g.PromptInjectionDefense,
			PIIMasking:             g.PIIMasking,
			HallucinationCheck:     g.HallucinationCheck,
			ZeroRetention:          g.ZeroRetention,
			InjectionThreshold:     g.InjectionThreshold,
		},
	})
}

func handleUpdateGuardrails(w http.ResponseWriter, r *http.Request, store runStore) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	gStore, ok := store.(repository.GuardrailsStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}

	var req GuardrailsDTO
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}

	updated, err := gStore.SaveGuardrails(r.Context(), repository.TenantGuardrails{
		TenantID:               scope.TenantID,
		PromptInjectionDefense: req.PromptInjectionDefense,
		PIIMasking:             req.PIIMasking,
		HallucinationCheck:     req.HallucinationCheck,
		ZeroRetention:          req.ZeroRetention,
		InjectionThreshold:     req.InjectionThreshold,
	})
	if err != nil {
		problem(w, http.StatusInternalServerError, "ai.guardrails_save_failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result": GuardrailsDTO{
			PromptInjectionDefense: updated.PromptInjectionDefense,
			PIIMasking:             updated.PIIMasking,
			HallucinationCheck:     updated.HallucinationCheck,
			ZeroRetention:          updated.ZeroRetention,
			InjectionThreshold:     updated.InjectionThreshold,
		},
	})
}
