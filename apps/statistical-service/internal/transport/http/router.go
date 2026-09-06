package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/statistical-service/internal/handler"
)

// NewRouter wires the statistical-service HTTP surface.
func NewRouter(h *handler.StatisticalHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("/api/statistical/report-definitions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListReportDefinitions(w, r)
		case http.MethodPost, http.MethodPut:
			h.UpsertReportDefinition(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/statistical/indicators", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListIndicators(w, r)
		case http.MethodPost, http.MethodPut:
			h.UpsertIndicator(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/statistical/submissions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListSubmissions(w, r)
		case http.MethodPost:
			h.CreateSubmission(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/statistical/submissions/{id}/submit", method("POST", h.SubmitSubmission))
	return mux
}

func method(verb string, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != verb {
			writeMethodNotAllowed(w, r)
			return
		}
		fn(w, r)
	}
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Write([]byte(`{"error":"method not allowed"}`))
}
