package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/iam-service/internal/handler"
)

// NewRouter wires HTTP routes for the IAM service.
func NewRouter(userHandler *handler.UserHandler, authHandler *handler.AuthHandler, policyHandler *handler.PolicyHandler, adminHandler *handler.AdminHandler) http.Handler {
	mux := http.NewServeMux()

	// Health
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

	// ── Auth API (public) ──
	mux.HandleFunc("/api/auth/login-page", method("GET", authHandler.LoginPage))
	mux.HandleFunc("/api/auth/login/password", method("POST", authHandler.LoginPassword))
	mux.HandleFunc("/api/auth/login/external", method("POST", authHandler.LoginExternal))
	mux.HandleFunc("/api/auth/callback/{provider_id}", method("GET", authHandler.CallbackProvider))
	mux.HandleFunc("/api/auth/callback", method("POST", authHandler.CallbackToken))
	mux.HandleFunc("/api/auth/refresh", method("POST", authHandler.Refresh))
	mux.HandleFunc("/api/auth/providers", method("GET", authHandler.ListProviders))
	mux.HandleFunc("/api/auth/consent", method("POST", authHandler.Consent))
	mux.HandleFunc("/api/auth/register", method("POST", authHandler.RegisterUser))

	// ── Admin API ──
	mux.HandleFunc("/api/admin/users", method("GET", adminHandler.ListUsers))
	mux.HandleFunc("/api/admin/users/create", method("POST", adminHandler.CreateUser))
	mux.HandleFunc("/api/admin/users/{id}", method("GET", adminHandler.GetUser))
	mux.HandleFunc("/api/admin/users/{id}/delete", method("DELETE", adminHandler.DeleteUser))

	// ── User API ──
	mux.HandleFunc("/api/iam/me", method("GET", userHandler.Me))

	// ── Policy API ──
	if policyHandler != nil {
		mux.HandleFunc("/api/policy/enforce", method("POST", policyHandler.Enforce))
		mux.HandleFunc("/api/admin/policies", method("GET", policyHandler.ListPolicies))
		mux.HandleFunc("/api/admin/policies/add", method("POST", policyHandler.AddPolicy))
		mux.HandleFunc("/api/admin/policies/remove", method("POST", policyHandler.RemovePolicy))
	}

	// ── Internal API (service-to-service) ──
	mux.HandleFunc("/internal/iam/users/by-subject/{subject}", method("GET", userHandler.GetBySubject))
	mux.HandleFunc("/internal/iam/users/by-id/{id}/context", method("GET", userHandler.GetContextByID))

	return mux
}

func method(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}
