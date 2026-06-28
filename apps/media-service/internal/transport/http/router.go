package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/media-service/internal/handler"
)

func NewRouter(mediaHandler *handler.MediaHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health/live", health("ok"))
	mux.HandleFunc("/health/ready", health("ready"))

	mux.HandleFunc("/api/media", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mediaHandler.Upload(w, r)
		} else {
			methodNotAllowed(w)
		}
	})

	mux.HandleFunc("/api/media/", func(w http.ResponseWriter, r *http.Request) {
		publicID, action, ok := parseMediaAction(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch {
		case action == "" && r.Method == http.MethodGet:
			mediaHandler.View(w, r, publicID)
		case action == "download" && r.Method == http.MethodGet:
			mediaHandler.Download(w, r, publicID)
		case action == "" && r.Method == http.MethodDelete:
			mediaHandler.Delete(w, r, publicID)
		default:
			methodNotAllowed(w)
		}
	})

	return mux
}

func parseMediaAction(urlPath string) (publicID string, action string, ok bool) {
	rest := strings.TrimPrefix(urlPath, "/api/media/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", false
	}
	if len(parts) == 1 {
		return parts[0], "", true
	}
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func health(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_, _ = w.Write([]byte(`{"error":{"code":"common.error.method_not_allowed","message":"Method not allowed"}}`))
}
