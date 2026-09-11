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
	mux.HandleFunc("GET /api/statistical/reports/{code}/run", h.RunReport)
	mux.HandleFunc("GET /api/statistical/reports/{code}/export", h.ExportReport)
	mux.HandleFunc("GET /api/statistical/catalogs", h.ListCatalogKinds)
	mux.HandleFunc("GET /api/statistical/catalogs/{kind}", h.ListCatalogItems)
	mux.HandleFunc("POST /api/statistical/catalogs/{kind}", h.UpsertCatalogItem)
	mux.HandleFunc("DELETE /api/statistical/catalogs/{kind}/{id}", h.DeactivateCatalogItem)
	mux.HandleFunc("/api/statistical/form-templates", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListFormTemplates(w, r)
		case http.MethodPost:
			h.UpsertFormTemplate(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("GET /api/statistical/form-templates/{code}/export", h.ExportFormTemplate)
	mux.HandleFunc("POST /api/statistical/form-templates/import", h.ImportFormTemplate)
	mux.HandleFunc("GET /api/statistical/dashboard", h.Dashboard)
	mux.HandleFunc("/api/statistical/score-results", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.ListScoreResults(w, r)
			return
		}
		h.CreateScoreResult(w, r)
	})
	mux.HandleFunc("GET /api/statistical/score-results/{id}", h.GetScoreResult)
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
