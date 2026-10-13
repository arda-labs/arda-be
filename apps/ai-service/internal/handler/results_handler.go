package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/ai-service/internal/sandbox/resultid"
)

// handleReadResult serves GET /api/ai/results/{resultId}?thread_id=... The
// result store is namespaced by tenant|user|thread, so a wrong thread or user
// is indistinguishable from an expired id (404 either way).
func handleReadResult(w http.ResponseWriter, r *http.Request, options RouterOptions) {
	if r.Method != http.MethodGet {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return
	}
	if r.Header.Get("X-Auth-Checked") != "true" {
		problem(w, http.StatusUnauthorized, "ai.auth_required")
		return
	}
	scope := scopeFromRequest(r)
	if scope.TenantID == "" || scope.ActorUserID == "" {
		problem(w, http.StatusUnauthorized, "ai.identity_context_required")
		return
	}
	if !hasPermission(r.Header.Get("X-Permissions"), assistantPermission) {
		problem(w, http.StatusForbidden, "ai.assistant_forbidden")
		return
	}
	// r.URL.Path is already percent-decoded: validate the decoded id, so an
	// encoded slash, query or fragment cannot slip through as part of the id.
	resultID := strings.TrimPrefix(r.URL.Path, "/api/ai/results/")
	if !resultid.Valid(resultID) {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	scope.ExternalThread = strings.TrimSpace(r.URL.Query().Get("thread_id"))
	if scope.ExternalThread == "" {
		problem(w, http.StatusBadRequest, "ai.invalid_request_body")
		return
	}
	if options.ResultReader == nil {
		problem(w, http.StatusNotFound, "ai.result_not_found")
		return
	}
	output, ok := options.ResultReader(scope, resultID)
	if !ok {
		problem(w, http.StatusNotFound, "ai.result_not_found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]json.RawMessage{"output": output})
}
