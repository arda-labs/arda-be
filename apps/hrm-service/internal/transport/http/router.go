package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/hrm-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func NewRouter(hrm *handler.HRMHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", health("ok"))
	mux.HandleFunc("/health/ready", health("ready"))

	mux.HandleFunc("/api/hrm/positions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			hrm.ListPositions(w, r)
		case http.MethodPost:
			hrm.CreatePosition(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/hrm/positions/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			hrm.UpdatePosition(w, r)
		case http.MethodDelete:
			hrm.DeletePosition(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/hrm/job-titles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			hrm.ListJobTitles(w, r)
		case http.MethodPost:
			hrm.CreateJobTitle(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/hrm/job-titles/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			hrm.UpdateJobTitle(w, r)
		case http.MethodDelete:
			hrm.DeleteJobTitle(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/hrm/org-units", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			hrm.ListOrgUnits(w, r)
		case http.MethodPost:
			hrm.CreateOrgUnit(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/hrm/org-units/tree", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, r)
			return
		}
		hrm.ListOrgUnits(w, r)
	})
	mux.HandleFunc("/api/hrm/org-units/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			hrm.UpdateOrgUnit(w, r)
		case http.MethodDelete:
			hrm.DeleteOrgUnit(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})

	mux.HandleFunc("/api/hrm/employees", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, r)
			return
		}
		hrm.ListEmployees(w, r)
	})

	mux.HandleFunc("/api/hrm/employee-registrations", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			hrm.ListEmployeeRegistrations(w, r)
		case http.MethodPost:
			hrm.CreateEmployeeRegistration(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/hrm/employee-registrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/submit") {
			return
		}
		switch r.Method {
		case http.MethodPut:
			hrm.UpdateEmployeeRegistration(w, r)
		default:
			methodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/hrm/employee-registrations/{id}/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, r)
			return
		}
		hrm.SubmitEmployeeRegistration(w, r)
	})

	return ardametadata.HTTPMiddleware(requireTenantScope(mux))
}

func requireTenantScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/health/") {
			next.ServeHTTP(w, r)
			return
		}
		metadata := ardametadata.FromOutgoing(r.Context())
		if metadata.AuthChecked != "true" {
			ardahttp.WriteProblem(w, r, http.StatusForbidden, ardaerrors.New(ardaerrors.CodeForbidden, "verified tenant scope is required"))
			return
		}
		if strings.TrimSpace(metadata.TenantID) == "" {
			ardahttp.WriteProblem(w, r, http.StatusBadRequest, ardaerrors.New(ardaerrors.CodeRequired, "verified tenant scope is required"))
			return
		}
		next.ServeHTTP(w, r)
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
