package handler

import (
	"encoding/json"
	"net/http"

	"github.com/arda-labs/arda/apps/notification-service/internal/service"
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
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	n, err := h.svc.Accept(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"notification_id": n.PublicID,
		"status":          n.Status,
	})
}

func (h *NotificationHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := h.svc.GetByPublicID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if n == nil {
		writeError(w, http.StatusNotFound, "notification not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"notification_id": n.PublicID,
		"tenant_id":       n.TenantID,
		"template_key":    n.TemplateKey,
		"status":          n.Status,
		"created_at":      n.CreatedAt,
		"updated_at":      n.UpdatedAt,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
