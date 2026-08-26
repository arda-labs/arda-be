package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func contextWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
}

func requireConversationScope(w http.ResponseWriter, r *http.Request, methods ...string) (tools.Context, bool) {
	allowed := false
	for _, method := range methods {
		if r.Method == method {
			allowed = true
			break
		}
	}
	if !allowed {
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
		return tools.Context{}, false
	}
	if r.Header.Get("X-Auth-Checked") != "true" {
		problem(w, http.StatusUnauthorized, "ai.auth_required")
		return tools.Context{}, false
	}
	if strings.TrimSpace(r.Header.Get("X-User-Id")) == "" || strings.TrimSpace(r.Header.Get("X-Tenant-Id")) == "" {
		problem(w, http.StatusUnauthorized, "ai.identity_context_required")
		return tools.Context{}, false
	}
	if !hasPermission(r.Header.Get("X-Permissions"), assistantPermission) {
		problem(w, http.StatusForbidden, "ai.assistant_forbidden")
		return tools.Context{}, false
	}
	return scopeFromRequest(r), true
}

func listConversations(w http.ResponseWriter, r *http.Request, store runStore, _ RouterOptions) {
	scope, ok := requireConversationScope(w, r, http.MethodGet)
	if !ok {
		return
	}
	reader, hasReader := store.(repository.ConversationReader)
	if !hasReader {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	items, err := reader.ListConversations(r.Context(), scope.TenantID, scope.ActorUserID, limit)
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "messages": []string{}, "result": items})
}

func conversationMessages(w http.ResponseWriter, r *http.Request, store runStore, _ RouterOptions) {
	scope, ok := requireConversationScope(w, r, http.MethodGet)
	if !ok {
		return
	}
	reader, hasReader := store.(repository.ConversationReader)
	if !hasReader {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/conversations/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "messages" || len(parts[0]) > 255 {
		problem(w, http.StatusNotFound, "ai.conversation_not_found")
		return
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	items, err := reader.ConversationMessages(r.Context(), scope.TenantID, scope.ActorUserID, parts[0], limit)
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "messages": []string{}, "result": items})
}

func deleteConversation(w http.ResponseWriter, r *http.Request, store runStore, _ RouterOptions) {
	scope, ok := requireConversationScope(w, r, http.MethodDelete)
	if !ok {
		return
	}
	mutator, hasMutator := store.(repository.ConversationMutator)
	if !hasMutator {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	threadID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/conversations/"), "/")
	if threadID == "" || len(threadID) > 255 || strings.Contains(threadID, "/") {
		problem(w, http.StatusNotFound, "ai.conversation_not_found")
		return
	}
	err := mutator.DeleteConversation(r.Context(), scope.TenantID, scope.ActorUserID, threadID)
	if err != nil {
		if errors.Is(err, repository.ErrConversationNotFound) {
			problem(w, http.StatusNotFound, "ai.conversation_not_found")
			return
		}
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "errors": []any{}, "messages": []string{}, "result": map[string]string{"threadId": threadID}})
}
