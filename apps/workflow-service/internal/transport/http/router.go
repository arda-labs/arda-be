package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/workflow-service/internal/handler"
)

func NewRouter(wfHandler *handler.WorkflowHandler) http.Handler {
	mux := http.NewServeMux()

	// Health check
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

	// Workflow APIs
	mux.HandleFunc("/api/v1/workflows/deploy", wfHandler.Deploy)
	mux.HandleFunc("/api/v1/workflows/start", wfHandler.Start)
	mux.HandleFunc("/api/v1/workflows/messages", wfHandler.PublishMessage)
	
	// Dynamic paths
	mux.HandleFunc("/api/v1/workflows/instances/", func(w http.ResponseWriter, r *http.Request) {
		// e.g. /api/v1/workflows/instances/{instanceKey}/cancel
		if r.Method == http.MethodPost && len(r.URL.Path) > len("/api/v1/workflows/instances/") {
			if r.URL.Path[len(r.URL.Path)-len("/cancel"):] == "/cancel" {
				wfHandler.Cancel(w, r)
				return
			}
		}

		// e.g. /api/v1/workflows/instances/mapping/{businessKey}
		if r.Method == http.MethodGet && len(r.URL.Path) > len("/api/v1/workflows/instances/mapping/") {
			wfHandler.GetMapping(w, r)
			return
		}

		http.NotFound(w, r)
	})

	return mux
}
