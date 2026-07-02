package http

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/handler"
)

// NewRouter wires auth-gateway HTTP routes.
func NewRouter(authHandler *handler.AuthHandler, bffHandler *handler.BFFHandler, cfg config.Config) http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/health/ready", method("GET", bffHandler.Ready))

	// ForwardAuth (Traefik)
	mux.HandleFunc("/auth/check", method("GET", authHandler.Check))

	// ── BFF endpoints for Kratos + Hydra flow ──

	// Hydra bridge
	mux.HandleFunc("/api/auth/login", method("GET", bffHandler.Login))
	mux.HandleFunc("/api/auth/consent", method("GET", bffHandler.Consent))
	mux.HandleFunc("/api/auth/start", method("GET", bffHandler.StartOAuth))
	mux.HandleFunc("/api/auth/kratos/accept-login", method("POST", bffHandler.AcceptKratosLogin))
	mux.HandleFunc("/api/auth/accept-consent", method("POST", bffHandler.AcceptConsent))

	// Token exchange (direct with Hydra). GET is the BFF-owned browser redirect;
	// POST remains for local/backward-compatible SPA callback flows.
	mux.HandleFunc("/api/auth/callback", bffHandler.Callback)

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
	mux.HandleFunc("/api/auth/step-up", method("POST", bffHandler.StepUp))
	mux.HandleFunc("/api/auth/me/sessions", method("GET", bffHandler.MeSessions))

	// Generic proxy for /api/* to iam-service
	mux.HandleFunc("/api/", bffHandler.Proxy)

	if !cfg.SlowRequestLogEnabled || cfg.SlowRequestLogThresholdMS <= 0 {
		return mux
	}
	return slowRequestLogger(mux, slog.Default(), time.Duration(cfg.SlowRequestLogThresholdMS)*time.Millisecond)
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

func slowRequestLogger(next http.Handler, logger *slog.Logger, threshold time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isEventStreamRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		duration := time.Since(start)
		if duration >= threshold {
			logger.Warn("slow request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration_ms", duration.Milliseconds(),
			)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func isEventStreamRequest(r *http.Request) bool {
	return r != nil && strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}
