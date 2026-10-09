package ardahttp

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// RecoveryMiddleware turns a panic in a handler into a catalogued 500 instead of
// a dropped connection.
//
// Without it, net/http recovers the panic itself, logs it on stderr as
// unstructured text, and closes the connection with no response body at all. For
// this platform that breaks two things at once: the RFC 7807 contract that
// check-problem-catalog.mjs enforces, and the client's ability to correlate the
// failure, because the X-Request-Id header never gets written.
//
// Mount it outermost, so a panic anywhere below it — including inside the CORS
// middleware, which must still be able to set Vary on the way out — is caught.
//
// ai-service streams SSE from a writer goroutine, and a panic there is not
// recoverable by this middleware: that goroutine owns the response and the
// client connection. ai-service already installs its own guard on that path;
// this covers the request-scoped handlers.
func RecoveryMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// http.ErrAbortHandler is the documented way to abandon a response
			// without a body; it is not a bug and must not be turned into a 500.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}

			requestID := RequestID(r)
			if logger != nil {
				// The stack goes to the server log, never to the client.
				logger.Error("panic recovered in http handler",
					"request_id", requestID,
					"method", r.Method,
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()),
				)
			}

			// If the handler already started a response there is no safe way to
			// change the status, so the connection is simply closed. The log line
			// above is the only record.
			if rw, ok := w.(interface{ Written() bool }); ok && rw.Written() {
				return
			}
			WriteProblem(w, r, http.StatusInternalServerError, ardaerrors.New(ardaerrors.CodeInternal, ""))
		}()
		next.ServeHTTP(w, r)
	})
}
