package identity

import (
	"net/http"
	"strings"
	"time"
)

// RequireServiceAuth protects internal service-to-service HTTP routes. It is
// intentionally strict: a missing or invalid caller token is rejected outright
// — there is no fallback, so an attacker who reaches the internal route cannot
// bypass service authentication by simply omitting the header.
//
// Semantics:
//   - missing token   → 401
//   - invalid/expired → 401
//   - wrong audience  → 401
//   - wrong source    → 403
//
// The middleware authenticates the *caller service* (e.g. ai-service). It does
// not authorize the delegated user — target services must perform their own
// resource-level authorization on the forwarded subject headers.
func RequireServiceAuth(secret, serviceName string, allowedSources map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimSpace(r.Header.Get(MetadataKey))
			if token == "" {
				http.Error(w, "service identity is required", http.StatusUnauthorized)
				return
			}
			claims, err := Verify(token, secret, serviceName, time.Now())
			if err != nil {
				http.Error(w, "invalid service identity", http.StatusUnauthorized)
				return
			}
			if _, ok := allowedSources[claims.Source]; !ok {
				http.Error(w, "forbidden service caller", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AllowedSources builds the set of callers permitted to reach a protected
// internal route. The AI service is the standard internal caller for the
// /internal/ai/* surface.
func AllowedSources(sources ...string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source) != "" {
			allowed[strings.TrimSpace(source)] = struct{}{}
		}
	}
	return allowed
}
