package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/service"
	"github.com/arda-labs/arda/libs/go/arda-auth/usercontext"
)

type NotificationHandler struct {
	svc *service.NotificationService
}

func NewNotificationHandler(svc *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in service.AcceptInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}

	n, err := h.svc.Accept(r.Context(), in)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, r, http.StatusAccepted, map[string]any{
		"notification_id": n.PublicID,
		"status":          n.Status,
	})
}

func (h *NotificationHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := h.svc.GetByPublicID(r.Context(), id)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if n == nil {
		writeError(w, r, http.StatusNotFound, "notification not found")
		return
	}

	writeJSON(w, r, http.StatusOK, map[string]any{
		"notification_id": n.PublicID,
		"tenant_id":       n.TenantID,
		"template_key":    n.TemplateKey,
		"status":          n.Status,
		"created_at":      n.CreatedAt,
		"updated_at":      n.UpdatedAt,
	})
}

func (h *NotificationHandler) ListInbox(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.ListInbox(r.Context(), tenantID, userID, limit)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, inboxItemJSON(item))
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": out})
}

func (h *NotificationHandler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	count, err := h.svc.UnreadCount(r.Context(), tenantID, userID)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"count": count})
}

func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	if err := h.svc.MarkRead(r.Context(), tenantID, userID, r.PathValue("id")); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	if err := h.svc.MarkAllRead(r.Context(), tenantID, userID); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NotificationHandler) Stream(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	count, err := h.svc.UnreadCount(r.Context(), tenantID, userID)
	if err != nil {
		writeError(w, r, http.StatusUnauthorized, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	writeSSE(w, "unread_count", map[string]int{"count": count})
	flusher.Flush()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("event: " + event + "\n"))
	_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
}

func requestUser(r *http.Request) (string, string) {
	uc := usercontext.FromHeaders(r.Header)
	userID := uc.UserID
	if userID == "" {
		userID = uc.Subject
	}
	return uc.TenantID, userID
}

func inboxItemJSON(item domain.InboxItem) map[string]any {
	var params map[string]any
	if len(item.Params) > 0 {
		_ = json.Unmarshal(item.Params, &params)
	}
	out := map[string]any{
		"id":        item.PublicID,
		"type":      item.Type,
		"titleKey":  item.TitleKey,
		"bodyKey":   item.BodyKey,
		"params":    params,
		"href":      item.Href,
		"readAt":    nil,
		"createdAt": item.CreatedAt,
	}
	if item.ReadAt != nil {
		out["readAt"] = item.ReadAt
	}
	return out
}
