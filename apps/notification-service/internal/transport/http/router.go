package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/notification-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func NewRouter(notificationHandler *handler.NotificationHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	mux.HandleFunc("GET /api/notifications", notificationHandler.ListInbox)
	mux.HandleFunc("GET /api/notifications/unread-count", notificationHandler.UnreadCount)
	mux.HandleFunc("GET /api/notifications/stream", notificationHandler.Stream)
	mux.HandleFunc("POST /api/notifications/{id}/read", notificationHandler.MarkRead)
	mux.HandleFunc("POST /api/notifications/read-all", notificationHandler.MarkAllRead)
	mux.HandleFunc("GET /api/notifications/push/vapid-public-key", notificationHandler.PushPublicKey)
	mux.HandleFunc("POST /api/notifications/push/subscribe", notificationHandler.SubscribePush)
	mux.HandleFunc("POST /api/notifications/push/unsubscribe", notificationHandler.UnsubscribePush)
	mux.HandleFunc("GET /api/notifications/templates", notificationHandler.ListTemplates)
	mux.HandleFunc("POST /api/notifications/templates", notificationHandler.UpsertTemplate)
	mux.HandleFunc("DELETE /api/notifications/templates/{id}", notificationHandler.DeleteTemplate)
	mux.HandleFunc("GET /api/notifications/senders", notificationHandler.ListSenders)
	mux.HandleFunc("POST /api/notifications/senders", notificationHandler.UpsertSender)
	mux.HandleFunc("GET /api/notifications/events", notificationHandler.ListEvents)
	mux.HandleFunc("GET /api/notifications/dlq", notificationHandler.ListDLQ)
	mux.HandleFunc("POST /api/notifications/dlq/{id}/retry", notificationHandler.RetryDLQ)
	mux.HandleFunc("DELETE /api/notifications/dlq/{id}", notificationHandler.DiscardDLQ)

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. The handler reuses the
	// same user-level scoping as the public inbox routes (see InternalAIListInbox).
	mux.Handle("GET /internal/ai/notifications", internalAIService(http.HandlerFunc(notificationHandler.InternalAIListInbox)))
	mux.Handle("GET /internal/ai/notifications/unread-count", internalAIService(http.HandlerFunc(notificationHandler.InternalAIUnreadCount)))

	return mux
}

// internalAIService authenticates the ai-service caller on the internal AI
// surface. Missing/invalid tokens are hard-rejected; the delegated subject
// (X-Tenant-Id, X-User-Id, ...) is forwarded by the caller, not trusted from
// browsers — this route is never exposed to them.
func internalAIService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ardahttp.WriteProblem(w, r, http.StatusServiceUnavailable, ardaerrors.New(ardaerrors.CodeInternal, "internal service identity is not configured"))
		})
	}
	return identity.RequireServiceAuth(secret, "notification-service", identity.AllowedSources("ai-service"))(next)
}
