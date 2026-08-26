package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

const copilotAgentID = "arda-assistant"

// Implements the CopilotKit v2 single-route envelope natively so the browser
// never needs a separate Node runtime: POST /api/copilotkit accepts
// {method, params, body}; "info" returns runtime metadata and "agent/run"
// streams AG-UI events for the body's RunAgentInput.
func copilotkitEndpoint(w http.ResponseWriter, r *http.Request, store runStore, resolver toolResolver, options RouterOptions) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	if r.Method == http.MethodGet || strings.TrimSpace(r.URL.Query().Get("info")) != "" {
		writeCopilotInfo(w)
		return
	}
	if r.Header.Get("X-Auth-Checked") != "true" {
		problem(w, http.StatusUnauthorized, "ai.auth_required")
		return
	}
	if strings.TrimSpace(r.Header.Get("X-User-Id")) == "" || strings.TrimSpace(r.Header.Get("X-Tenant-Id")) == "" {
		problem(w, http.StatusUnauthorized, "ai.identity_context_required")
		return
	}
	if !hasPermission(r.Header.Get("X-Permissions"), assistantPermission) {
		problem(w, http.StatusForbidden, "ai.assistant_forbidden")
		return
	}

	var envelope struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Body   runInput        `json:"body"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&envelope); err != nil {
		problem(w, http.StatusBadRequest, "ai.invalid_copilotkit_envelope")
		return
	}

	switch envelope.Method {
	case "info":
		writeCopilotInfo(w)
	case "agent/run":
		runInputFlow(w, r, store, resolver, envelope.Body, options)
	default:
		problem(w, http.StatusNotFound, "ai.copilotkit_unknown_method")
	}
}

func writeCopilotInfo(w http.ResponseWriter) {
	description := "Arda assistant with tenant-scoped read tools and human approval."
	writeJSON(w, http.StatusOK, map[string]any{
		"version": "arda-v1",
		"agents": map[string]any{
			copilotAgentID: map[string]any{
				"name":        copilotAgentID,
				"description": description,
			},
		},
		"audioFileTranscriptionEnabled": false,
	})
}
