package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
)

// NewRouter wires the deposit-service HTTP surface.
func NewRouter(h *handler.DepositHandler) http.Handler {
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
	mux.HandleFunc("/api/deposit/savings", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListSavings(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/deposit/savings/open", method("POST", h.OpenSavings))
	mux.HandleFunc("/api/deposit/savings/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/settle") && r.Method == http.MethodPost:
			h.SettleSavings(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/deposit/interbank", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListInterbankDeposits(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
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
