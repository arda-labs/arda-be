package ardahttp

import "net/http"

// LimitRequestBody caps how many bytes a handler can read from r.Body at n
// bytes. It is the shared replacement for hand-rolled
// `r.Body = http.MaxBytesReader(w, r.Body, n)` calls so every service enforces
// the same request-size contract.
//
// When the limit is exceeded the wrapped body returns *http.MaxBytesError on
// the next Read (http.MaxBytesReader also asks the server to close the
// connection), so handlers must map that error to 413 Request Entity Too Large
// (or reject the request) instead of treating it as invalid JSON. A
// non-positive limit is a no-op: callers must pass the explicit budget for the
// endpoint and never rely on this helper to disable limiting.
func LimitRequestBody(w http.ResponseWriter, r *http.Request, n int64) {
	if n <= 0 {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, n)
}

// LimitBodyMiddleware applies LimitRequestBody(n) to every request before
// handing it to next. Reads still fail lazily with *http.MaxBytesError, so the
// wrapped handler owns the 413 response; the middleware only removes the need
// to repeat the limit in each handler.
func LimitBodyMiddleware(n int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LimitRequestBody(w, r, n)
		next.ServeHTTP(w, r)
	})
}
