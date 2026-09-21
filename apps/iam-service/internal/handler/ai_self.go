package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

type aiSelfReader interface {
	GetUserContextByIDForTenant(context.Context, string, string) (*domain.UserContext, error)
}

// AISelfDisplay is only mounted behind internalAIService. It exposes labels
// for the delegated actor's active tenant, never the full personal profile or
// memberships in other tenants.
func (h *UserHandler) AISelfDisplay(w http.ResponseWriter, r *http.Request) {
	serveAISelfDisplay(w, r, h.svc)
}

func serveAISelfDisplay(w http.ResponseWriter, r *http.Request, reader aiSelfReader) {
	userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if r.Header.Get("X-Auth-Checked") != "true" || userID == "" || tenantID == "" {
		respondError(w, r, http.StatusForbidden, "verified actor and tenant are required")
		return
	}
	allowed := false
	for _, p := range strings.FieldsFunc(r.Header.Get("X-Permissions"), func(r rune) bool { return r == ',' || r == ' ' }) {
		if p == "ai.assistant.use" || p == "superadmin" {
			allowed = true
		}
	}
	if !allowed {
		respondError(w, r, http.StatusForbidden, "assistant permission is required")
		return
	}
	profile, err := reader.GetUserContextByIDForTenant(r.Context(), userID, tenantID)
	if err != nil || profile == nil || profile.UserID != userID || profile.ActiveTenantID != tenantID {
		respondError(w, r, http.StatusForbidden, "active tenant membership could not be verified")
		return
	}
	for _, membership := range profile.TenantMemberships {
		if membership.TenantID == tenantID && membership.Status == "ACTIVE" && membership.TenantStatus == "ACTIVE" {
			respondCanonicalJSON(w, r, http.StatusOK, map[string]any{
				"user":   map[string]any{"id": userID, "name": profile.DisplayName},
				"tenant": map[string]any{"id": tenantID, "code": membership.TenantCode, "name": membership.TenantName},
			})
			return
		}
	}
	respondError(w, r, http.StatusForbidden, "active tenant membership could not be verified")
}
