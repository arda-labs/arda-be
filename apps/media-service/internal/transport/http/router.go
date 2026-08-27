package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/media-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func NewRouter(mediaHandler *handler.MediaHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health/live", health("ok"))
	mux.HandleFunc("/health/ready", health("ready"))

	mux.HandleFunc("/api/media", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mediaHandler.Upload(w, r)
		} else {
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/media/files/init-upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mediaHandler.InitUpload(w, r)
			return
		}
		methodNotAllowed(w, r)
	})
	mux.HandleFunc("/api/media/files/", func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/media/files/"
		rest := strings.TrimPrefix(r.URL.Path, prefix)
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) == 2 && parts[1] == "complete-upload" && parts[0] != "" && r.Method == http.MethodPost {
			mediaHandler.CompleteUpload(w, r, parts[0])
			return
		}
		methodNotAllowed(w, r)
	})

	mux.HandleFunc("/api/media/public/", func(w http.ResponseWriter, r *http.Request) {
		publicID, action, ok := parsePublicMediaAction(r.URL.Path)
		if !ok {
			ardahttp.WriteProblem(w, r, http.StatusNotFound, ardaerrors.New(ardaerrors.CodeNotFound, "public media route not found"))
			return
		}
		switch {
		case action == "" && r.Method == http.MethodGet:
			mediaHandler.PublicView(w, r, publicID)
		case action == "download" && r.Method == http.MethodGet:
			mediaHandler.PublicDownload(w, r, publicID)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/media/", func(w http.ResponseWriter, r *http.Request) {
		publicID, action, ok := parseMediaAction(r.URL.Path)
		if !ok {
			ardahttp.WriteProblem(w, r, http.StatusNotFound, ardaerrors.New(ardaerrors.CodeNotFound, "media route not found"))
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
			methodNotAllowed(w, r)
		}
	})

	return mux
}

func parsePublicMediaAction(urlPath string) (publicID string, action string, ok bool) {
	rest := strings.TrimPrefix(urlPath, "/api/media/public/")
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

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "Method not allowed"))
}
