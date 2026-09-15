package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
)

const (
	aiInboxDefaultLimit = 10
	aiInboxMaxLimit     = 20
)

// aiInboxItem is the redacted inbox shape exposed to the AI SDK. The `params`
// map is free-form and can carry personal data (names, amounts, document
// references), so it is dropped here; the response allowlist in
// contracts/ai-internal/notification-v1.json drops it again as defense in depth.
type aiInboxItem struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	TitleKey  string     `json:"titleKey"`
	BodyKey   string     `json:"bodyKey"`
	Href      string     `json:"href"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

func toAIInboxItems(items []domain.InboxItem) []aiInboxItem {
	redacted := make([]aiInboxItem, 0, len(items))
	for _, item := range items {
		redacted = append(redacted, aiInboxItem{
			ID:        item.PublicID,
			Type:      item.Type,
			TitleKey:  item.TitleKey,
			BodyKey:   item.BodyKey,
			Href:      item.Href,
			ReadAt:    item.ReadAt,
			CreatedAt: item.CreatedAt,
		})
	}
	return redacted
}

// InternalAIListInbox serves GET /internal/ai/notifications for ai-service.
// The signed caller assertion is verified by the router's internalAIService
// middleware; the delegated subject (X-Tenant-Id, X-User-Id / X-User-Subject)
// is forwarded by ai-service and re-used for the same service-level scoping as
// the public ListInbox route.
func (h *NotificationHandler) InternalAIListInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := aiInboxDefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > aiInboxMaxLimit {
			writeError(w, r, http.StatusBadRequest, "limit must be an integer between 1 and 20")
			return
		}
		limit = parsed
	}
	tenantID, userID := requestUser(r)
	items, err := h.svc.ListInbox(r.Context(), tenantID, userID, limit)
	if err != nil {
		writeNotificationError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": toAIInboxItems(items)})
}

// InternalAIUnreadCount serves GET /internal/ai/notifications/unread-count
// for ai-service.
func (h *NotificationHandler) InternalAIUnreadCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	tenantID, userID := requestUser(r)
	count, err := h.svc.UnreadCount(r.Context(), tenantID, userID)
	if err != nil {
		writeNotificationError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]int{"count": count})
}
