package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
	"github.com/arda-labs/arda/apps/notification-service/internal/service"
	"github.com/arda-labs/arda/libs/go/arda-auth/usercontext"
	ardacrypto "github.com/arda-labs/arda/libs/go/arda-crypto"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type NotificationHandler struct {
	svc                *service.NotificationService
	secret             string
	mailTester         *service.MailTester
	streamHub          *StreamHub
	publishInboxChange func(tenantID, userID string) error
}

func NewNotificationHandler(svc *service.NotificationService, secret string) *NotificationHandler {
	return &NotificationHandler{svc: svc, secret: secret}
}

// SetMailTester enables POST /api/notifications/test-send.
func (h *NotificationHandler) SetMailTester(t *service.MailTester) {
	h.mailTester = t
}

func (h *NotificationHandler) SetStreamHub(hub *StreamHub) { h.streamHub = hub }

func (h *NotificationHandler) SetInboxChangePublisher(publish func(tenantID, userID string) error) {
	h.publishInboxChange = publish
}

// SendTest handles POST /api/notifications/test-send — sends one email through
// the tenant's active sender to verify config + template (X2).
func (h *NotificationHandler) SendTest(w http.ResponseWriter, r *http.Request) {
	if h.mailTester == nil {
		writeError(w, r, http.StatusServiceUnavailable, "mail tester is not configured")
		return
	}
	tenantID, _ := requestUser(r)
	var in struct {
		EventCode string         `json:"event_code"`
		Recipient string         `json:"recipient"`
		Locale    string         `json:"locale"`
		Params    map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.mailTester.SendTest(r.Context(), tenantID, in.EventCode, in.Locale, in.Recipient, in.Params); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NotificationHandler) ListInbox(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.ListInbox(r.Context(), tenantID, userID, limit)
	if err != nil {
		writeNotificationError(w, r, err)
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
		writeNotificationError(w, r, err)
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
	h.publishInboxChanged(tenantID, userID)
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	if err := h.svc.MarkAllRead(r.Context(), tenantID, userID); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	h.publishInboxChanged(tenantID, userID)
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NotificationHandler) ListPreferences(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	items, err := h.svc.ListPreferences(r.Context(), tenantID, userID)
	if err != nil {
		writeNotificationError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

func (h *NotificationHandler) SavePreference(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	var p domain.NotificationPreference
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.svc.SavePreference(r.Context(), tenantID, userID, p); err != nil {
		writeNotificationError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NotificationHandler) publishInboxChanged(tenantID, userID string) {
	if h.publishInboxChange != nil {
		_ = h.publishInboxChange(tenantID, userID)
	}
}

func (h *NotificationHandler) PushPublicKey(w http.ResponseWriter, r *http.Request) {
	key := h.svc.VAPIDPublicKey()
	if key == "" {
		writeError(w, r, http.StatusServiceUnavailable, "web push is not configured")
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"publicKey": key})
}

func (h *NotificationHandler) SubscribePush(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	var in service.PushSubscribeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.svc.SubscribePush(r.Context(), tenantID, userID, r.UserAgent(), in); err != nil {
		if errors.Is(err, service.ErrPushEndpointOwned) {
			writeNotificationError(w, r, err)
			return
		}
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NotificationHandler) UnsubscribePush(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.svc.UnsubscribePush(r.Context(), tenantID, userID, in.Endpoint); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (h *NotificationHandler) Stream(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	if h.streamHub == nil {
		writeError(w, r, http.StatusServiceUnavailable, "notification stream is not configured")
		return
	}
	lastSeq, hasLastSeq, err := parseLastEventID(r.Header.Get("Last-Event-ID"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid Last-Event-ID")
		return
	}
	leaseID, err := h.svc.AcquireStreamLease(r.Context(), tenantID, userID, maxStreamsPerUser, streamLeaseTTL)
	if errors.Is(err, service.ErrStreamLeaseLimit) {
		w.Header().Set("Retry-After", "15")
		writeError(w, r, http.StatusTooManyRequests, "notification stream limit reached")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "notification stream lease is unavailable")
		return
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.svc.ReleaseStreamLease(cleanupCtx, tenantID, userID, leaseID)
	}()
	events, unsubscribe, err := h.streamHub.Subscribe(tenantID, userID)
	if errors.Is(err, ErrStreamLimit) {
		w.Header().Set("Retry-After", "15")
		writeError(w, r, http.StatusTooManyRequests, "notification stream limit reached")
		return
	}
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "notification stream is not configured")
		return
	}
	defer unsubscribe()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	cursor := lastSeq
	latest, err := h.svc.LatestInboxEventSeq(r.Context(), tenantID, userID)
	if err != nil {
		return
	}
	if !hasLastSeq || cursor > latest {
		cursor = latest
	}
	if hasLastSeq {
		var keep bool
		cursor, keep = h.writeCatchup(w, flusher, r, tenantID, userID, cursor)
		if !keep {
			return
		}
		if latest < cursor {
			latest = cursor
		}
	}
	count, err := h.svc.UnreadCount(r.Context(), tenantID, userID)
	if err != nil {
		return
	}
	if latest < cursor {
		latest = cursor
	}
	if !writeSSE(w, flusher, latest, "unread_count", map[string]int{"count": count}) {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if err := h.svc.RenewStreamLease(r.Context(), tenantID, userID, leaseID, streamLeaseTTL); err != nil {
				return
			}
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		case signal := <-events:
			switch signal.Type {
			case "resolved":
				latest, err := h.svc.LatestInboxEventSeq(r.Context(), tenantID, userID)
				if err != nil {
					return
				}
				if latest < cursor {
					latest = cursor
				}
				payload := map[string]string{"event_id": signal.EventID, "entity_type": signal.EntityType, "entity_id": signal.EntityID}
				if !writeSSE(w, flusher, latest, "resolved", payload) {
					return
				}
			case "state_changed":
				latest, err := h.svc.LatestInboxEventSeq(r.Context(), tenantID, userID)
				if err != nil {
					return
				}
				count, err := h.svc.UnreadCount(r.Context(), tenantID, userID)
				if err != nil {
					return
				}
				if latest < cursor {
					latest = cursor
				}
				if !writeSSE(w, flusher, latest, "unread_count", map[string]int{"count": count}) {
					return
				}
			default:
				var keep bool
				cursor, keep = h.writeCatchup(w, flusher, r, tenantID, userID, cursor)
				if !keep {
					return
				}
			}
		}
	}
}

func (h *NotificationHandler) writeCatchup(w http.ResponseWriter, flusher http.Flusher, r *http.Request, tenantID, userID string, after int64) (int64, bool) {
	items, err := h.svc.ListInboxAfter(r.Context(), tenantID, userID, after, 101)
	if err != nil {
		return after, false
	}
	more := len(items) > 100
	if more {
		items = items[:100]
	}
	cursor, ok := writeCatchupItems(w, flusher, items, after)
	if !ok {
		return cursor, false
	}
	if more {
		latest, err := h.svc.LatestInboxEventSeq(r.Context(), tenantID, userID)
		if err != nil {
			return cursor, false
		}
		if latest < cursor {
			latest = cursor
		}
		payload := map[string]string{"reason": "catchup_limit", "action": "fetch_rest_inbox"}
		if !writeSSE(w, flusher, latest, "resync_required", payload) {
			return cursor, false
		}
		cursor = latest
	}
	return cursor, true
}

func writeCatchupItems(w http.ResponseWriter, flusher http.Flusher, items []domain.InboxItem, after int64) (int64, bool) {
	cursor := after
	for _, item := range items {
		if !writeSSE(w, flusher, item.EventSeq, "inbox_changed", inboxItemJSON(item)) {
			return cursor, false
		}
		if item.EventSeq > cursor {
			cursor = item.EventSeq
		}
	}
	return cursor, true
}

func parseLastEventID(raw string) (int64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	seq, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seq < 0 {
		return 0, true, fmt.Errorf("invalid event sequence")
	}
	return seq, true, nil
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, eventSeq int64, event string, v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", eventSeq, event, b); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func requestUser(r *http.Request) (string, string) {
	uc := usercontext.FromHeaders(r.Header)
	userID := uc.UserID
	if userID == "" {
		userID = uc.Subject
	}
	return uc.TenantID, userID
}

func writeNotificationError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := ardaerrors.CodeInternal
	message := "Notification service failed"

	switch {
	case errors.Is(err, service.ErrTenantScopeRequired):
		status = http.StatusConflict
		code = ardaerrors.CodeTenantScopeRequired
		message = "A verified tenant scope is required"
	case errors.Is(err, service.ErrTenantMigrationRequired):
		status = http.StatusConflict
		code = ardaerrors.CodeTenantMigrationRequired
		message = "The current tenant requires migration before notifications can be used"
	case errors.Is(err, service.ErrUserContextRequired):
		status = http.StatusBadRequest
		code = ardaerrors.CodeUserContextRequired
		message = "Authenticated user context is required"
	case errors.Is(err, service.ErrPushEndpointOwned):
		status = http.StatusConflict
		code = ardaerrors.CodeConflict
		message = "This push endpoint is already registered to another account"
	}

	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}

func inboxItemJSON(item domain.InboxItem) map[string]any {
	var params map[string]any
	if len(item.Params) > 0 {
		_ = json.Unmarshal(item.Params, &params)
	}
	if params == nil {
		params = map[string]any{}
	}
	out := map[string]any{
		"id":             item.PublicID,
		"type":           item.Type,
		"titleKey":       item.TitleKey,
		"bodyKey":        item.BodyKey,
		"params":         params,
		"href":           item.Href,
		"readAt":         nil,
		"createdAt":      item.CreatedAt,
		"entityType":     item.EntityType,
		"entityId":       item.EntityID,
		"resolvedAt":     item.ResolvedAt,
		"resolvedReason": item.ResolvedReason,
		"supersededAt":   item.SupersededAt,
		"expiresAt":      item.ExpiresAt,
		"locale":         item.Locale,
		"priority":       item.Priority,
		"eventSeq":       item.EventSeq,
	}
	if item.ReadAt != nil {
		out["readAt"] = item.ReadAt
	}
	return out
}

// ListTemplates handles GET /api/notifications/templates (X2).
func (h *NotificationHandler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := requestUser(r)
	items, err := h.svc.ListTemplates(r.Context(), tenantID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// UpsertTemplate handles POST /api/notifications/templates (X2).
func (h *NotificationHandler) UpsertTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	var in repository.NotificationTemplate
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	created, err := h.svc.UpsertTemplate(r.Context(), tenantID, userID, &in)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, created)
}

// DeleteTemplate handles DELETE /api/notifications/templates/{id} (X2).
func (h *NotificationHandler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := requestUser(r)
	if err := h.svc.DeleteTemplate(r.Context(), tenantID, r.PathValue("id")); err != nil {
		writeError(w, r, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

// ListEmailDesigns handles GET /api/notifications/designs.
func (h *NotificationHandler) ListEmailDesigns(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := requestUser(r)
	items, err := h.svc.ListEmailDesigns(r.Context(), tenantID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// UpsertEmailDesign handles POST /api/notifications/designs.
func (h *NotificationHandler) UpsertEmailDesign(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	var in repository.EmailDesign
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	created, err := h.svc.UpsertEmailDesign(r.Context(), tenantID, userID, &in)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, created)
}

// DeleteEmailDesign handles DELETE /api/notifications/designs/{id}.
func (h *NotificationHandler) DeleteEmailDesign(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := requestUser(r)
	if err := h.svc.DeleteEmailDesign(r.Context(), tenantID, r.PathValue("id")); err != nil {
		writeError(w, r, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

// ListSenders handles GET /api/notifications/senders (X2).
func (h *NotificationHandler) ListSenders(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := requestUser(r)
	items, err := h.svc.ListSenders(r.Context(), tenantID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// UpsertSender handles POST /api/notifications/senders — the password (if
// present) is encrypted at rest with the service secret (X2).
func (h *NotificationHandler) UpsertSender(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	var in struct {
		repository.SenderConfig
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid json")
		return
	}
	in.SenderConfig.TenantID = tenantID
	if in.Password != "" {
		encrypted, err := ardacrypto.Encrypt(in.Password, h.secret)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "could not encrypt the password")
			return
		}
		in.SenderConfig.PasswordEnc = encrypted
	}
	created, err := h.svc.UpsertSender(r.Context(), tenantID, userID, &in.SenderConfig)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusCreated, created)
}

// ListEvents handles GET /api/notifications/events — the notification event
// registry (fe_common #17 / fe_bpm #7).
func (h *NotificationHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := requestUser(r)
	items, err := h.svc.ListEvents(r.Context(), tenantID)
	if err != nil {
		writeNotificationError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// ListDLQ handles GET /api/notifications/dlq — pending dead-lettered events.
func (h *NotificationHandler) ListDLQ(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := requestUser(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.ListOutboxDLQ(r.Context(), tenantID, limit)
	if err != nil {
		writeNotificationError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

// RetryDLQ handles POST /api/notifications/dlq/{id}/retry.
func (h *NotificationHandler) RetryDLQ(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := requestUser(r)
	if err := h.svc.ReplayOutboxDLQ(r.Context(), tenantID, r.PathValue("id"), userID); err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

// DiscardDLQ handles DELETE /api/notifications/dlq/{id}.
func (h *NotificationHandler) DiscardDLQ(w http.ResponseWriter, r *http.Request) {
	tenantID, _ := requestUser(r)
	if err := h.svc.DiscardOutboxDLQ(r.Context(), tenantID, r.PathValue("id")); err != nil {
		writeError(w, r, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}
