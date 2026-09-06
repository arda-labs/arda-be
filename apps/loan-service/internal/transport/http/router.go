package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// NewRouter wires the loan-service HTTP surface. Adjustment routes are
// generated from the shared kind list so adding a flow never touches here.
func NewRouter(h *handler.LoanHandler, d *handler.DisbursementHandler, c *handler.CollectionHandler, a *handler.AccrualHandler, p *handler.ProvisionHandler, kinds []string) http.Handler {
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
	mux.HandleFunc("/api/loan/contracts/{id}/dossier", method("GET", h.GetDossier))

	// Disbursements (P1b drawdown flow, LNM.300.02)
	mux.HandleFunc("/api/loan/disbursements", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			d.ListDisbursements(w, r)
		case http.MethodPost:
			d.CreateDisbursement(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/loan/disbursements/{id}/submit", method("POST", d.SubmitDisbursement))

	// Accruals (P1b.4b): EOD job trigger + read API
	mux.HandleFunc("/internal/jobs/accrual-daily", a.RunDailyAccrual)
	mux.HandleFunc("/internal/jobs/provision-daily", p.RunProvision)
	mux.HandleFunc("/api/loan/accruals", method("GET", a.ListAccruals))

	// Collections (P1b.4a receipt flow, LNM.301.02)
	mux.HandleFunc("/api/loan/collections", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			c.ListCollections(w, r)
		case http.MethodPost:
			c.CreateCollection(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/loan/collections/{id}/submit", method("POST", c.SubmitCollection))

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

	// Products
	mux.HandleFunc("/api/loan/products", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListProducts(w, r)
		case http.MethodPost, http.MethodPut:
			h.UpsertProduct(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	// VFU (ủy thác)
	mux.HandleFunc("/api/loan/vfu/parties", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListVfuParties(w, r)
		case http.MethodPost:
			h.CreateVfuParty(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/loan/vfu/mandates", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListVfuMandates(w, r)
		case http.MethodPost:
			h.CreateVfuMandate(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/loan/vfu/plans", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.ListVfuPlans(w, r)
		case http.MethodPost:
			h.CreateVfuPlan(w, r)
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
