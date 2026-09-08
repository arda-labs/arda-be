package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/finance-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// NewRouter wires HTTP routes for the finance service. Posting endpoints do
// NOT live here — the gRPC PostingService owns them (contract v0.2 §1);
// HTTP is read/config surface for the finance remote.
func NewRouter(financeHandler *handler.FinanceHandler, coaHandler *handler.CoaHandler, postingHandler *handler.PostingHandler, cashHandler *handler.CashHandler, postingCaseHandler *handler.PostingCaseHandler) http.Handler {
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
	mux.HandleFunc("/api/finance/accounts/{id}", method("GET", financeHandler.GetAccount))
	mux.HandleFunc("/api/finance/accounts/", func(w http.ResponseWriter, r *http.Request) {
		financeHandler.GetAccount(w, r)
	})

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. Resource-level scoping
	// still applies inside the handler (tenant from X-Tenant-Id).
	mux.Handle("/internal/ai/accounts/{id}", internalAIService(method("GET", financeHandler.GetAccount)))

	// Trial balance (journal-aggregated)
	mux.HandleFunc("/api/finance/trial-balance", method("GET", financeHandler.TrialBalance))

	// VCM cash treasury (P2.4b)
	mux.HandleFunc("/api/finance/cash", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			cashHandler.RecordCash(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/cash-position", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			cashHandler.Position(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})

	// ── Posting stack (journal read + preview + opening balances) ──
	mux.HandleFunc("/api/finance/journal-entries", method("GET", postingHandler.ListJournalEntries))
	mux.HandleFunc("/api/finance/posting/validate", method("POST", postingHandler.ValidatePosting))
	mux.HandleFunc("/api/finance/posting-cases", method("POST", postingCaseHandler.CreatePostingCase))
	mux.HandleFunc("/api/finance/opening-balances", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			postingHandler.ListOpeningBalances(w, r)
		case http.MethodPost:
			postingHandler.UpsertOpeningBalance(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})

	// Accounting configuration
	mux.HandleFunc("/api/finance/accounting/process-configs", method("GET", financeHandler.ListProcessConfigs))
	mux.HandleFunc("/api/finance/accounting/account-classifications", method("GET", financeHandler.ListAccountClassifications))
	mux.HandleFunc("/api/finance/accounting/journal-definitions", method("GET", financeHandler.ListJournalDefinitions))
	mux.HandleFunc("/api/finance/accounting/regulatory-accounts", method("GET", financeHandler.ListRegulatoryAccounts))
	mux.HandleFunc("/api/finance/accounting/internal-accounts", method("GET", financeHandler.ListInternalAccounts))

	// ── COA v2 definition layer (versions / chart / class maps / structures) ──
	mux.HandleFunc("/api/finance/coa/versions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			coaHandler.ListVersions(w, r)
		case http.MethodPost, http.MethodPut:
			coaHandler.UpsertVersion(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/coa/accounts", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			coaHandler.ListAccounts(w, r)
		case http.MethodPost, http.MethodPut:
			coaHandler.UpsertAccount(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/coa/class-maps", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			coaHandler.ListClassMaps(w, r)
		case http.MethodPost, http.MethodPut:
			coaHandler.UpsertClassMap(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/finance/coa/resolve", method("GET", coaHandler.ResolveClassification))
	mux.HandleFunc("/api/finance/coa/structures", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			coaHandler.ListStructures(w, r)
		case http.MethodPost, http.MethodPut:
			coaHandler.UpsertStructure(w, r)
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
