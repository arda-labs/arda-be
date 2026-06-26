package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/handler"
)

// NewRouter wires HTTP routes for the finance service.
func NewRouter(financeHandler *handler.FinanceHandler, approvalHandler *handler.ApprovalHandler) http.Handler {
	mux := http.NewServeMux()

	// Health
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

	// Accounts
	mux.HandleFunc("/api/finance/accounts", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			financeHandler.ListAccounts(w, r)
		case http.MethodPost:
			financeHandler.CreateAccount(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/finance/accounts/{id}/balance", method("GET", financeHandler.GetAccountBalance))
	mux.HandleFunc("/api/finance/accounts/", func(w http.ResponseWriter, r *http.Request) {
		financeHandler.GetAccount(w, r)
	})

	// Transactions
	mux.HandleFunc("/api/finance/transactions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			financeHandler.ListTransactions(w, r)
		case http.MethodPost:
			financeHandler.CreateTransaction(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/finance/transactions/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/reverse") && r.Method == http.MethodPost {
			financeHandler.ReverseTransaction(w, r)
			return
		}
		if r.Method == http.MethodGet {
			financeHandler.GetTransaction(w, r)
			return
		}
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	})

	// Trial balance
	mux.HandleFunc("/api/finance/trial-balance", method("GET", financeHandler.TrialBalance))

	// ── Approvals ──
	mux.HandleFunc("/api/finance/approvals", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			approvalHandler.ListPending(w, r)
		case http.MethodPost:
			approvalHandler.Create(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/finance/approvals/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/approve") && r.Method == http.MethodPost:
			approvalHandler.Approve(w, r)
		case strings.HasSuffix(path, "/reject") && r.Method == http.MethodPost:
			approvalHandler.Reject(w, r)
		case strings.HasSuffix(path, "/cancel") && r.Method == http.MethodPost:
			approvalHandler.Cancel(w, r)
		case r.Method == http.MethodGet:
			approvalHandler.Get(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})

	return mux
}

func method(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}
