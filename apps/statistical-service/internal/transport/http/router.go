package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/statistical-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

// NewRouter wires the statistical-service HTTP surface.
func NewRouter(h *handler.StatisticalHandler, internalAIHandler *handler.InternalAIHandler, reportingJob *handler.ReportingJobHandler, indicatorResults *handler.IndicatorResultHandler, presentation *handler.PresentationHandler) http.Handler {
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
	mux.HandleFunc("/api/statistical/submissions/{id}", method("GET", h.GetSubmission))
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
	mux.HandleFunc("/api/statistical/indicator-results", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			indicatorResults.ListIndicatorResults(w, r)
		case http.MethodPost, http.MethodPut:
			indicatorResults.UpsertIndicatorResult(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("POST /api/statistical/indicators/compute", indicatorResults.ComputeIndicators)

	// Presentation: chart contract + rendered documents (pdf/xlsx/html).
	mux.HandleFunc("GET /api/statistical/reports/{code}/chart", presentation.ReportChart)
	mux.HandleFunc("GET /api/statistical/reports/{code}/document", presentation.ReportDocument)
	mux.HandleFunc("GET /api/statistical/indicators/document", presentation.IndicatorDocument)
	mux.HandleFunc("/api/statistical/score-results", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.ListScoreResults(w, r)
			return
		}
		h.CreateScoreResult(w, r)
	})
	mux.HandleFunc("GET /api/statistical/score-results/{id}", h.GetScoreResult)
	mux.HandleFunc("/api/statistical/import-transactions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.ListImportTransactions(w, r)
			return
		}
		h.UpsertImportTransaction(w, r)
	})
	mux.HandleFunc("POST /api/statistical/import-transactions/{id}/submit", h.SubmitImportTransaction)
	mux.HandleFunc("GET /api/statistical/cmms/results", h.ListCmmsResults)
	mux.HandleFunc("POST /api/statistical/cmms/run", h.RunCmms)

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. Only report metadata is
	// exposed (definitions, indicators, submission status): raw report rows,
	// query ids and payloads are never reachable from this surface. Scoping is
	// re-enforced in the handler and the repository.
	mux.Handle("GET /internal/ai/report-definitions", internalAIService(http.HandlerFunc(internalAIHandler.InternalAIListReportDefinitions)))
	mux.Handle("GET /internal/ai/indicators", internalAIService(http.HandlerFunc(internalAIHandler.InternalAIListIndicators)))
	mux.Handle("GET /internal/ai/submissions", internalAIService(http.HandlerFunc(internalAIHandler.InternalAIListSubmissions)))
	mux.Handle("GET /internal/ai/report-run", internalAIService(http.HandlerFunc(internalAIHandler.InternalAIRunReport)))
	mux.Handle("GET /internal/ai/indicator-results", internalAIService(http.HandlerFunc(internalAIHandler.InternalAIListIndicatorResults)))

	// Internal reporting ETL job: platform EOD calls this (no gateway policy;
	// network-policy protected like the other /internal/jobs/* steps). Tenant
	// travels in X-Tenant-Id, the business date in ?to_date=.
	mux.HandleFunc("/internal/jobs/report-extract-daily", method("POST", reportingJob.RunReportExtractDaily))

	return mux
}

// internalAIService authenticates the ai-service caller on the internal AI
// surface. Missing/invalid tokens are hard-rejected; the delegated subject
// (X-Tenant-Id) is forwarded by the caller, never trusted from browsers —
// these routes are not exposed through auth-gateway.
func internalAIService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal service identity is not configured", http.StatusServiceUnavailable)
		})
	}
	return identity.RequireServiceAuth(secret, "statistical-service", identity.AllowedSources("ai-service"))(next)
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
