package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/capital-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

// NewRouter wires the capital-service HTTP surface.
func NewRouter(h *handler.CapitalHandler, internalAIHandler *handler.InternalAIHandler, rep *handler.InternalReportingHandler) http.Handler {
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
	mux.HandleFunc("/api/capital/fund-types", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListFundTypes(w, r)
		case http.MethodPost:
			h.CreateFundType(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("PUT /api/capital/fund-types/{id}", h.UpdateFundType)
	mux.HandleFunc("DELETE /api/capital/fund-types/{id}", h.DeactivateFundType)
	mux.HandleFunc("/api/capital/products", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListProducts(w, r)
		case http.MethodPost, http.MethodPut:
			h.UpsertProduct(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("DELETE /api/capital/products/{id}", h.DeactivateProduct)
	mux.HandleFunc("/api/capital/contracts", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListContracts(w, r)
		case http.MethodPost:
			h.CreateContract(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("GET /api/capital/contracts/{id}", h.GetContractDetail)
	mux.HandleFunc("POST /api/capital/contracts/{id}/amendments", h.SubmitAmendment)
	mux.HandleFunc("POST /api/capital/contracts/{id}/movements", h.RecordMovement)
	mux.HandleFunc("GET /api/capital/reports/fund-source-statement", h.GetFundSourceStatement)
	mux.HandleFunc("GET /api/capital/reports/fund-source-transactions", h.GetFundSourceTransactions)

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. Resource-level scoping
	// still applies inside the handler (tenant from X-Tenant-Id + org scope +
	// repository filter), so browsers can never reach these routes.
	mux.Handle("GET /internal/ai/fund-types", internalAIService(http.HandlerFunc(internalAIHandler.InternalAIListFundTypes)))
	mux.Handle("GET /internal/ai/products", internalAIService(http.HandlerFunc(internalAIHandler.InternalAIListProducts)))
	mux.Handle("GET /internal/ai/contracts", internalAIService(http.HandlerFunc(internalAIHandler.InternalAIListContracts)))

	// Internal reporting surface: statistical-service's ETL (signed caller).
	mux.Handle("GET /internal/reporting/capital-contracts", internalReportingService(http.HandlerFunc(rep.InternalReportingContracts)))
	mux.Handle("GET /internal/reporting/capital-movements", internalReportingService(http.HandlerFunc(rep.InternalReportingMovements)))

	return mux
}

// internalAIService authenticates the ai-service caller on the internal AI
// surface. Missing/invalid tokens are hard-rejected; the delegated subject
// (X-Tenant-Id, org headers) is forwarded by the caller, never trusted from a
// browser — this route is not exposed through auth-gateway.
func internalAIService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal service identity is not configured", http.StatusServiceUnavailable)
		})
	}
	return identity.RequireServiceAuth(secret, "capital-service", identity.AllowedSources("ai-service"))(next)
}

// internalReportingService authenticates the statistical-service caller on the
// reporting ETL surface (same signed-assertion contract, distinct source).
func internalReportingService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal service identity is not configured", http.StatusServiceUnavailable)
		})
	}
	return identity.RequireServiceAuth(secret, "capital-service", identity.AllowedSources("statistical-service"))(next)
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Write([]byte(`{"error":"method not allowed"}`))
}
