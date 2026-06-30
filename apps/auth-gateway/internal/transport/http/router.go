package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/handler"
)

// NewRouter wires auth-gateway HTTP routes.
func NewRouter(authHandler *handler.AuthHandler, bffHandler *handler.BFFHandler) http.Handler {
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

	// ForwardAuth (Traefik)
	mux.HandleFunc("/auth/check", method("GET", authHandler.Check))

	// ── BFF endpoints for Kratos + Hydra flow ──

	// Hydra bridge
	mux.HandleFunc("/api/auth/kratos/accept-login", method("POST", bffHandler.AcceptKratosLogin))
	mux.HandleFunc("/api/auth/accept-consent", method("POST", bffHandler.AcceptConsent))

	// Token exchange (direct with Hydra)
	mux.HandleFunc("/api/auth/callback", method("POST", bffHandler.ExchangeCode))

	// Kratos proxy
	mux.HandleFunc("/api/kratos/whoami", bffHandler.KratosWhoami)
	mux.HandleFunc("/api/kratos/login/api", method("GET", bffHandler.KratosCreateLoginAPIFlow))
	mux.HandleFunc("/api/kratos/login/browser", method("GET", bffHandler.KratosCreateLoginFlow))
	mux.HandleFunc("/api/kratos/login/flows", method("GET", bffHandler.KratosGetLoginFlow))
	mux.HandleFunc("/api/kratos/login", method("POST", bffHandler.KratosSubmitLogin))
	mux.HandleFunc("/api/kratos/settings/browser", method("GET", bffHandler.KratosCreateSettingsFlow))
	mux.HandleFunc("/api/kratos/settings/flows", method("GET", bffHandler.KratosGetSettingsFlow))
	mux.HandleFunc("/api/kratos/settings", method("POST", bffHandler.KratosSubmitSettings))
	mux.HandleFunc("/api/kratos/recovery/browser", method("GET", bffHandler.KratosCreateRecoveryFlow))
	mux.HandleFunc("/api/kratos/recovery/flows", method("GET", bffHandler.KratosGetRecoveryFlow))
	mux.HandleFunc("/api/kratos/recovery", method("POST", bffHandler.KratosSubmitRecovery))
	mux.HandleFunc("/api/kratos/verification/browser", method("GET", bffHandler.KratosCreateVerificationFlow))
	mux.HandleFunc("/api/kratos/verification/flows", method("GET", bffHandler.KratosGetVerificationFlow))
	mux.HandleFunc("/api/kratos/verification", method("POST", bffHandler.KratosSubmitVerification))

	// Session management
	mux.HandleFunc("/api/auth/me", method("GET", bffHandler.Me))
	mux.HandleFunc("/api/auth/logout", method("POST", bffHandler.Logout))
	mux.HandleFunc("/api/auth/me/sessions", method("GET", bffHandler.MeSessions))

	// Generic proxy for /api/* to iam-service
	mux.HandleFunc("/api/", bffHandler.Proxy)

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
