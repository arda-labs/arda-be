package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

type Router struct {
	customerHandler  *handler.CustomerHandler
	amendmentHandler *handler.AmendmentHandler
}

func NewRouter(customerHandler *handler.CustomerHandler, amendmentHandler *handler.AmendmentHandler) http.Handler {
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

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. Resource-level scoping
	// still applies inside the handler (see InternalAIGetCustomer).
	mux.Handle("/internal/ai/customers/{id}", internalAIService(http.HandlerFunc(customerHandler.InternalAIGetCustomer)))

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
