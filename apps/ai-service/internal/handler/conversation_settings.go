package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

type conversationSettingsStore interface {
	GetConversationRetention(ctx context.Context, tenantID string) (int, error)
	SaveConversationRetention(ctx context.Context, tenantID string, months int) error
}

type conversationRetentionRequest struct {
	TrashRetentionMonths int `json:"trash_retention_months"`
}

func handleConversationSettings(w http.ResponseWriter, r *http.Request, store runStore) {
	scope, ok := identityScope(w, r)
	if !ok {
		return
	}
	settings, ok := store.(conversationSettingsStore)
	if !ok {
		problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		months, err := settings.GetConversationRetention(r.Context(), scope.TenantID)
		if err != nil {
			problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
			return
		}
		writeResultEnvelope(w, map[string]any{"trash_retention_months": months, "maximum_months": 12})
	case http.MethodPut:
		var input conversationRetentionRequest
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || ensureEOF(decoder) != nil || input.TrashRetentionMonths < 1 || input.TrashRetentionMonths > 12 {
			problem(w, http.StatusBadRequest, "ai.invalid_request_body")
			return
		}
		if err := settings.SaveConversationRetention(r.Context(), scope.TenantID, input.TrashRetentionMonths); err != nil {
			problem(w, http.StatusServiceUnavailable, "ai.persistence_unavailable")
			return
		}
		writeResultEnvelope(w, map[string]any{"trash_retention_months": input.TrashRetentionMonths, "maximum_months": 12})
	default:
		problem(w, http.StatusMethodNotAllowed, "ai.method_not_allowed")
	}
}
