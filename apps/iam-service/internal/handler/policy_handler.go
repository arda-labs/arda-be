package handler

import (
	"encoding/json"
	"net/http"

	"github.com/arda-labs/arda/apps/iam-service/internal/policy"
)

// PolicyHandler exposes policy management endpoints.
type PolicyHandler struct {
	enf *policy.Enforcer
}

// NewPolicyHandler creates a policy handler.
// Accepts nil — policy endpoints will return disabled status.
func NewPolicyHandler(enf *policy.Enforcer) *PolicyHandler {
	return &PolicyHandler{enf: enf}
}

func (h *PolicyHandler) isDisabled() bool {
	return h.enf == nil
}

// Enforce checks if a subject can perform an action.
func (h *PolicyHandler) Enforce(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subject string         `json:"sub"`
		Object  string         `json:"obj"`
		Action  string         `json:"act"`
		Env     map[string]any `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	ok, err := h.enf.Enforce(req.Subject, req.Object, req.Action, req.Env)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
		"allowed": ok,
	})
}

// ListPolicies returns all policies.
func (h *PolicyHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	// Not implemented — in production, read from Casbin adapter
	respondJSON(w, r, http.StatusOK, map[string]any{
		"policies": []any{},
	})
}

// AddPolicy adds a policy rule.
func (h *PolicyHandler) AddPolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subject string `json:"sub"`
		Object  string `json:"obj"`
		Action  string `json:"act"`
		Effect  string `json:"eft"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	eff := req.Effect
	if eff == "" {
		eff = "allow"
	}

	if err := h.enf.AddPolicy(req.Subject, req.Object, req.Action, eff); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{"status": "added"})
}

// RemovePolicy removes a policy rule.
func (h *PolicyHandler) RemovePolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Subject string `json:"sub"`
		Object  string `json:"obj"`
		Action  string `json:"act"`
		Effect  string `json:"eft"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.enf.RemovePolicy(req.Subject, req.Object, req.Action, req.Effect); err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{"status": "removed"})
}
