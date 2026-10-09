package ardahttp

import (
	"io"
	"log/slog"
	"net/http"
)

// HandlerChain applies the HTTP hardening that every service in this platform
// shares, in the order the layers need to run:
//
//  1. MetricsMiddleware wraps the mux, so it observes the final status code and
//     the latency of everything below it.
//  2. LimitBodyMiddlewareByPrefix caps the request body, raising the budget only
//     for the routes listed in bodyLimits. It sits above the mux so an oversize
//     Content-Length is refused before a handler can buffer it.
//  3. RecoveryMiddleware is outermost, so a panic anywhere below still produces
//     a catalogued problem+json 500 with a request id instead of a dropped
//     connection, and so the CORS headers a handler already set survive.
//
// Every service used to compose its own chain inline, which is how a body limit
// ended up written but never called: a helper with zero call sites is a helper
// nobody wired up. Routing every service through one constructor makes the
// limit a property of the platform rather than of each main.go.
//
// bodyLimits may be nil, in which case the service default applies.
// extraRenderers is forwarded to MetricsMiddleware unchanged; ai-service uses it
// to publish its own AI counters on /metrics.
func HandlerChain(appName string, bodyLimits []BodyLimitOverride, next http.Handler, extraRenderers ...func(io.Writer)) http.Handler {
	var h http.Handler = MetricsMiddleware(appName, next, extraRenderers...)
	h = LimitBodyMiddlewareByPrefix(DefaultMaxBodyBytes, bodyLimits, h)
	h = RecoveryMiddleware(slog.Default(), h)
	return h
}
