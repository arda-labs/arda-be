package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/notification-service/internal/handler"
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

	mux.HandleFunc("POST /internal/notifications", notificationHandler.Create)
	mux.HandleFunc("GET /internal/notifications/{id}", notificationHandler.Get)
	mux.HandleFunc("POST /api/notifications", notificationHandler.Create)
	mux.HandleFunc("GET /api/notifications/{id}", notificationHandler.Get)

	return mux
}
