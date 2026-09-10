package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/capital-service/internal/handler"
)

// NewRouter wires the capital-service HTTP surface.
func NewRouter(h *handler.CapitalHandler) http.Handler {
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
	return mux
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Write([]byte(`{"error":"method not allowed"}`))
}
