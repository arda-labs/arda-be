package http

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/handler"
)

type Router struct {
	customerHandler   *handler.CustomerHandler
	amendmentHandler  *handler.AmendmentHandler
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

	mux.HandleFunc("/api/v1/customers", customerHandler.CreateCustomer)
	mux.HandleFunc("/api/crm/customers", customerHandler.Customers)
	mux.HandleFunc("/api/crm/customers/", r.customerByID)

	return mux
}

func (r *Router) customerByID(w http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.URL.Path, "/adjustments") {
		r.amendmentHandler.Route(w, req)
		return
	}
	r.customerHandler.CustomerByID(w, req)
}
