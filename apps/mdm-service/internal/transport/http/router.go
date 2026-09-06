package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/mdm-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// NewRouter wires every registered catalog plus the versioned interest-rate
// domain. Catalog routes are generated from the service registry so adding a
// catalog never requires touching this file.
func NewRouter(catalogHandler *handler.CatalogHandler, rateHandler *handler.InterestRateHandler, catalogs []string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health/live", health("ok"))
	mux.HandleFunc("/health/ready", health("ready"))

	for _, catalog := range catalogs {
		registerCatalog(mux, catalogHandler, catalog)
	}

	mux.HandleFunc("/api/mdm/interest-rates", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			rateHandler.List(w, r)
		case http.MethodPost:
			rateHandler.Create(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/mdm/interest-rates/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			rateHandler.Get(w, r)
		case http.MethodPut:
			rateHandler.Update(w, r)
		case http.MethodDelete:
			rateHandler.Delete(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/mdm/interest-rates/{id}/tiers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			rateHandler.ListTiers(w, r)
		case http.MethodPost:
			rateHandler.CreateTier(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/mdm/interest-rates/{id}/tiers/{tierId}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			http.NotFound(w, r)
		case http.MethodPut:
			rateHandler.UpdateTier(w, r)
		case http.MethodDelete:
			rateHandler.DeleteTier(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	return mux
}

func registerCatalog(mux *http.ServeMux, h *handler.CatalogHandler, catalog string) {
	pattern := "/api/mdm/" + catalog
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.List(catalog)(w, r)
		case http.MethodPost:
			h.Create(catalog)(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc(pattern+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			h.Get(catalog)(w, r)
		case http.MethodPut:
			h.Update(catalog)(w, r)
		case http.MethodDelete:
			h.Delete(catalog)(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
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
