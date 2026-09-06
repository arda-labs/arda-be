package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// NewRouter wires the loan-service HTTP surface. Adjustment routes are
// generated from the shared kind list so adding a flow never touches here.
func NewRouter(h *handler.LoanHandler, kinds []string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health/live", health("ok"))
	mux.HandleFunc("/health/ready", health("ready"))

	// Contracts
	mux.HandleFunc("/api/loan/contracts", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListContracts(w, r)
		case http.MethodPost:
			h.CreateContract(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/loan/contracts/{id}", method("GET", h.GetContract))
	mux.HandleFunc("/api/loan/contracts/{id}/submit", method("POST", h.SubmitContract))

	// Agreements (disbursements)
	mux.HandleFunc("/api/loan/agreements", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListAgreements(w, r)
		case http.MethodPost:
			h.CreateAgreement(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	// Repay plans
	mux.HandleFunc("/api/loan/repay-plans", method("GET", h.ListRepayPlans))

	// Mortgages + collaterals
	mux.HandleFunc("/api/loan/mortgages", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListMortgages(w, r)
		case http.MethodPost:
			h.CreateMortgage(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/loan/collaterals", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListCollaterals(w, r)
		case http.MethodPost:
			h.CreateCollateral(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/loan/contract-collaterals", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListContractCollaterals(w, r)
		case http.MethodPost:
			h.AttachContractCollateral(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	// Adjustment flows — uniform per kind
	for _, kind := range kinds {
		base := "/api/loan/adjustments/" + kind
		mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				h.ListAdjustments(w, r)
			case http.MethodPost:
				h.CreateAdjustment(w, r)
			default:
				methodNotAllowed(w, r)
			}
		})
		mux.HandleFunc(base+"/{id}", method("GET", h.GetAdjustment))
		mux.HandleFunc(base+"/{id}/submit", method("POST", h.SubmitAdjustment))
	}

	return mux
}

func method(verb string, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != verb {
			methodNotAllowed(w, r)
			return
		}
		fn(w, r)
	}
}

func health(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	}
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}
