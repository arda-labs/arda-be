package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/document-service/internal/handler"
)

func NewRouter(h *handler.DocumentHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", health("ok"))
	mux.HandleFunc("/health/ready", health("ready"))
	mux.HandleFunc("GET /api/documents/capabilities", h.Capabilities)
	mux.HandleFunc("POST /api/documents/extract", h.Extract)
	return mux
}

func health(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	}
}
