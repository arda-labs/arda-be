package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

// NewRouter wires the deposit-service HTTP surface.
func NewRouter(h *handler.DepositHandler, ai *handler.InternalAIHandler) http.Handler {
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
	mux.HandleFunc("/api/deposit/products", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListProducts(w, r)
		case http.MethodPost, http.MethodPut:
			h.UpsertProduct(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/deposit/product-requests", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListProductRequests(w, r)
		case http.MethodPost:
			h.SubmitProductRequest(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("GET /api/deposit/product-requests/{id}", h.GetProductRequest)
	mux.HandleFunc("/api/deposit/savings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListSavings(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("POST /api/deposit/savings/open", h.OpenSavings)
	mux.HandleFunc("GET /api/deposit/savings/{code}", h.GetSavingsDetail)
	mux.HandleFunc("POST /api/deposit/savings/{code}/settle", h.SubmitSettlement)
	mux.HandleFunc("POST /api/deposit/savings/{code}/deposit", h.SubmitAdditional)
	mux.HandleFunc("POST /api/deposit/savings/{code}/interest", h.SubmitSavingsInterest)
	mux.HandleFunc("/api/deposit/rates", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListInterestRates(w, r)
		case http.MethodPost:
			h.SubmitRateRequest(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("GET /api/deposit/rates/{id}", h.GetRateRequest)
	mux.HandleFunc("GET /api/deposit/interest-ops", h.GetInterestOpsByCase)
	mux.HandleFunc("/api/deposit/batch-interest", method("POST", h.SubmitBatchInterest))
	mux.HandleFunc("GET /api/deposit/reports/deposit-statement", h.GetDepositStatement)
	mux.HandleFunc("GET /api/deposit/reports/deposit-transactions", h.GetDepositTransactions)
	mux.HandleFunc("GET /api/deposit/reports/interbank-statement", h.GetInterbankStatement)
	mux.HandleFunc("GET /api/deposit/reports/interbank-transactions", h.GetInterbankTransactions)
	mux.HandleFunc("/internal/jobs/deposit-accrual-daily", method("POST", h.RunAccrualDaily))
	mux.HandleFunc("/api/deposit/interbank", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListInterbankDeposits(w, r)
		case http.MethodPost:
			h.CreateInterbankDeposit(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("GET /api/deposit/interbank/{id}", h.GetInterbankDetail)
	mux.HandleFunc("POST /api/deposit/interbank/{id}/movements", h.SubmitIBMMovement)
	mux.HandleFunc("/api/deposit/ibm-products", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListIBMProducts(w, r)
		case http.MethodPost, http.MethodPut:
			h.UpsertIBMProduct(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. Resource-level scoping
	// still applies inside every handler (tenant re-check + org filter).
	mux.Handle("GET /internal/ai/savings", internalAIService(http.HandlerFunc(ai.InternalAIListSavings)))
	mux.Handle("GET /internal/ai/savings/{code}", internalAIService(http.HandlerFunc(ai.InternalAIGetSavingsDetail)))
	mux.Handle("GET /internal/ai/interest-rates", internalAIService(http.HandlerFunc(ai.InternalAIListInterestRates)))

	return mux
}

// internalAIService authenticates the ai-service caller on the internal AI
// surface. Missing/invalid tokens are hard-rejected; the delegated subject
// (X-Tenant-Id, ...) is forwarded by the caller, not trusted from browsers —
// this route is never exposed to them.
func internalAIService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal service identity is not configured", http.StatusServiceUnavailable)
		})
	}
	return identity.RequireServiceAuth(secret, "deposit-service", identity.AllowedSources("ai-service"))(next)
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
