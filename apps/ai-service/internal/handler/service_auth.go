package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

// ServiceAuthMiddleware verifies the gateway's workload identity separately
// from the delegated user/tenant headers. In spike mode it is optional so the
// protocol endpoint can be tested locally; production always requires it.
func ServiceAuthMiddleware(next http.Handler, secret string, required bool) http.Handler {
	if strings.TrimSpace(secret) == "" && !required {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Kubernetes probes must remain available before workload identity is
		// established; application routes are still fail-closed below.
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}
		claims, err := identity.Verify(r.Header.Get(identity.MetadataKey), secret, "ai-service", time.Now())
		if err != nil || claims.Source != "auth-gateway" {
			problem(w, http.StatusUnauthorized, "ai.service_auth_required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
