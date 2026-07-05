package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/hrm-service/internal/handler"
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
			methodNotAllowed(w)
		}
	})
	mux.HandleFunc("/api/hrm/positions/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			hrm.UpdatePosition(w, r)
		case http.MethodDelete:
			hrm.DeletePosition(w, r)
		default:
			methodNotAllowed(w)
		}
	})

	mux.HandleFunc("/api/hrm/job-titles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			hrm.ListJobTitles(w, r)
		case http.MethodPost:
			hrm.CreateJobTitle(w, r)
		default:
			methodNotAllowed(w)
		}
	})
	mux.HandleFunc("/api/hrm/job-titles/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			hrm.UpdateJobTitle(w, r)
		case http.MethodDelete:
			hrm.DeleteJobTitle(w, r)
		default:
			methodNotAllowed(w)
		}
	})

	mux.HandleFunc("/api/hrm/org-units", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			hrm.ListOrgUnits(w, r)
		case http.MethodPost:
			hrm.CreateOrgUnit(w, r)
		default:
			methodNotAllowed(w)
		}
	})
	mux.HandleFunc("/api/hrm/org-units/tree", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
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
			methodNotAllowed(w)
		}
	})

	mux.HandleFunc("/api/hrm/employees", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
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
			methodNotAllowed(w)
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
			methodNotAllowed(w)
		}
	})
	mux.HandleFunc("/api/hrm/employee-registrations/{id}/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		hrm.SubmitEmployeeRegistration(w, r)
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
