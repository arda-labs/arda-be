package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/platform-service/internal/handler"
)

func NewRouter(platformHandler *handler.PlatformHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health/live", health("ok"))
	mux.HandleFunc("/health/ready", health("ready"))

	mux.HandleFunc("/api/platform/parameters", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListParameters(w, r)
		case http.MethodPost, http.MethodPut:
			platformHandler.UpsertParameter(w, r)
		default:
			methodNotAllowed(w)
		}
	})

	mux.HandleFunc("/api/platform/lookups", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListLookupCategories(w, r)
		case http.MethodPost, http.MethodPut:
			platformHandler.UpsertLookupCategory(w, r)
		default:
			methodNotAllowed(w)
		}
	})

	mux.HandleFunc("/api/platform/lookups/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/values") {
			methodNotAllowed(w)
			return
		}
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListLookupValues(w, r)
		case http.MethodPost:
			platformHandler.CreateLookupValue(w, r)
		default:
			methodNotAllowed(w)
		}
	})

	mux.HandleFunc("/api/platform/organizations", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListOrganizations(w, r)
		case http.MethodPost:
			platformHandler.CreateOrganization(w, r)
		default:
			methodNotAllowed(w)
		}
	})

	mux.HandleFunc("/api/platform/geo/admin-units", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			platformHandler.ListGeoAdminUnits(w, r)
		case http.MethodPost, http.MethodPut:
			platformHandler.UpsertGeoAdminUnit(w, r)
		default:
			methodNotAllowed(w)
		}
	})

	return mux
}

func health(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
}
