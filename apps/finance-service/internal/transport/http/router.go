package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/finance-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
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
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/accounts/{id}/balance", method("GET", financeHandler.GetAccountBalance))
	mux.HandleFunc("/api/finance/accounts/{id}", method("GET", financeHandler.GetAccount))
	mux.HandleFunc("/api/finance/accounts/", func(w http.ResponseWriter, r *http.Request) {
		financeHandler.GetAccount(w, r)
	})

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. Resource-level scoping
	// still applies inside the handler (tenant from X-Tenant-Id).
	mux.Handle("/internal/ai/accounts/{id}", internalAIService(method("GET", financeHandler.GetAccount)))

	// Business operation transactions
	mux.HandleFunc("/api/finance/incoming-transactions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			financeHandler.ListIncomingTransactions(w, r)
		case http.MethodPost:
			financeHandler.CreateIncomingTransaction(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/incoming-transactions/{id}", method("GET", financeHandler.GetOperationTransaction))
	mux.HandleFunc("/api/finance/outgoing-transactions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			financeHandler.ListOutgoingTransactions(w, r)
		case http.MethodPost:
			financeHandler.CreateOutgoingTransaction(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/outgoing-transactions/{id}", method("GET", financeHandler.GetOperationTransaction))

	// Transactions
	mux.HandleFunc("/api/finance/transactions/search", method("GET", financeHandler.SearchTransactions))
	mux.HandleFunc("/api/finance/transactions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			financeHandler.ListTransactions(w, r)
		case http.MethodPost:
			financeHandler.CreateTransaction(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/transactions/{id}", method("GET", financeHandler.GetTransaction))
	mux.HandleFunc("/api/finance/transactions/{id}/reverse", method("POST", financeHandler.ReverseTransaction))

	// Trial balance
	mux.HandleFunc("/api/finance/trial-balance", method("GET", financeHandler.TrialBalance))

	// Accounting configuration
	mux.HandleFunc("/api/finance/accounting/process-configs", method("GET", financeHandler.ListProcessConfigs))
	mux.HandleFunc("/api/finance/accounting/account-classifications", method("GET", financeHandler.ListAccountClassifications))
	mux.HandleFunc("/api/finance/accounting/journal-definitions", method("GET", financeHandler.ListJournalDefinitions))
	mux.HandleFunc("/api/finance/accounting/regulatory-accounts", method("GET", financeHandler.ListRegulatoryAccounts))
	mux.HandleFunc("/api/finance/accounting/internal-accounts", method("GET", financeHandler.ListInternalAccounts))

	// ── Approvals ──
	mux.HandleFunc("/api/finance/approvals", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			approvalHandler.ListPending(w, r)
		case http.MethodPost:
			approvalHandler.Create(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/approvals/{id}", method("GET", approvalHandler.Get))
	mux.HandleFunc("/api/finance/approvals/{id}/approve", method("POST", approvalHandler.Approve))
	mux.HandleFunc("/api/finance/approvals/{id}/reject", method("POST", approvalHandler.Reject))
	mux.HandleFunc("/api/finance/approvals/{id}/cancel", method("POST", approvalHandler.Cancel))
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
			writeMethodNotAllowed(w, r)
		}
	})

	return ardametadata.HTTPMiddleware(mux)
}

func method(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeMethodNotAllowed(w, r)
			return
		}
		next(w, r)
	}
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}

// internalAIService authenticates the ai-service caller on the internal AI
// surface. Missing/invalid tokens are hard-rejected; the delegated subject
// (X-Tenant-Id, X-User-Id, ...) is forwarded by the caller, not trusted from
// browsers — this route is never exposed to them.
func internalAIService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ardahttp.WriteProblem(w, r, http.StatusServiceUnavailable, ardaerrors.New(ardaerrors.CodeInternal, "internal service identity is not configured"))
		})
	}
	return identity.RequireServiceAuth(secret, "finance-service", identity.AllowedSources("ai-service"))(next)
}
