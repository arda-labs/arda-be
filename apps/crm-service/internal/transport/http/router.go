package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

type Router struct {
	customerHandler  *handler.CustomerHandler
	amendmentHandler *handler.AmendmentHandler
}

func NewRouter(customerHandler *handler.CustomerHandler, amendmentHandler *handler.AmendmentHandler, projectHandler *handler.ProjectHandler, reportHandler *handler.ReportHandler, memberHandler *handler.MemberHandler) http.Handler {
	r := &Router{
		customerHandler:  customerHandler,
		amendmentHandler: amendmentHandler,
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	mux.HandleFunc("/api/crm/customers", customerHandler.Customers)
	mux.HandleFunc("/api/crm/customers/", r.customerByID)
	mux.HandleFunc("/api/crm/project-types", func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			projectHandler.ListProjectTypes(w, req)
		case http.MethodPost, http.MethodPut:
			projectHandler.UpsertProjectType(w, req)
		default:
			writeMethodNotAllowed(w, req)
		}
	})
	mux.HandleFunc("/api/crm/customers/{id}/risk-flags", func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			projectHandler.ListRiskFlags(w, req)
		case http.MethodPost:
			projectHandler.AddRiskFlag(w, req)
		default:
			writeMethodNotAllowed(w, req)
		}
	})
	mux.HandleFunc("/api/crm/projects", func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodGet:
			projectHandler.ListProjects(w, req)
		case http.MethodPost:
			projectHandler.CreateProject(w, req)
		default:
			writeMethodNotAllowed(w, req)
		}
	})
	mux.HandleFunc("/api/crm/projects/{id}", projectHandler.ProjectByID)
	mux.HandleFunc("/api/crm/projects/{id}/members", projectHandler.ProjectMembers)
	mux.HandleFunc("DELETE /api/crm/projects/{id}/members/{memberId}", projectHandler.ProjectMemberByID)
	mux.HandleFunc("GET /api/crm/reports/customers", reportHandler.GetCustomerReport)

	// QTDND membership: register + capital-movement pipeline (maker-checker).
	mux.HandleFunc("/api/crm/members", memberHandler.Members)
	mux.HandleFunc("GET /api/crm/members/{id}", memberHandler.MemberByID)
	mux.HandleFunc("PUT /api/crm/members/{id}", memberHandler.MemberByID)
	mux.HandleFunc("/api/crm/member-requests", memberHandler.MemberRequests)
	mux.HandleFunc("POST /api/crm/member-requests/{id}/decision", memberHandler.MemberRequestDecision)

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. Resource-level scoping
	// still applies inside the handler (see InternalAIGetCustomer).
	mux.Handle("/internal/ai/customers/{id}", internalAIService(http.HandlerFunc(customerHandler.InternalAIGetCustomer)))

	// Internal reporting surface: statistical-service's reporting ETL reads the
	// tenant customer slice (signed caller; tenant re-checked in the handler).
	mux.Handle("GET /internal/reporting/customers", internalReportingService(http.HandlerFunc(customerHandler.InternalReportingCustomers)))
	mux.Handle("GET /internal/reporting/members", internalReportingService(http.HandlerFunc(memberHandler.InternalReportingMembers)))
	mux.Handle("GET /internal/reporting/member-requests", internalReportingService(http.HandlerFunc(memberHandler.InternalReportingMemberRequests)))

	return ardametadata.HTTPMiddleware(mux)
}

func (r *Router) customerByID(w http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.URL.Path, "/adjustments") {
		r.amendmentHandler.Route(w, req)
		return
	}
	r.customerHandler.CustomerByID(w, req)
}

// internalAIService authenticates the ai-service caller on the internal AI
// surface. Missing/invalid tokens are hard-rejected; the delegated subject
// (X-Tenant-Id, X-User-Id, ...) is forwarded by the caller, not trusted from
// browsers — this route is never exposed to them.
func internalAIService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal service identity is not configured", http.StatusServiceUnavailable)
		})
	}
	return identity.RequireServiceAuth(secret, "crm-service", identity.AllowedSources("ai-service"))(next)
}

// internalReportingService authenticates the statistical-service caller on the
// reporting ETL surface (same signed-assertion contract, distinct source).
func internalReportingService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal service identity is not configured", http.StatusServiceUnavailable)
		})
	}
	return identity.RequireServiceAuth(secret, "crm-service", identity.AllowedSources("statistical-service"))(next)
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}
