package handler

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// respondJSON writes a JSON response.
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// respondError writes a JSON error response.
func respondError(w http.ResponseWriter, status int, msg string) {
	respondErrorCode(w, status, ardaerrors.CodeForStatus(status), msg)
}

func respondErrorCode(w http.ResponseWriter, status int, code, msg string) {
	err := ardaerrors.New(code, msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ardaerrors.Response{Error: *err})
}

func respondRequestErrorCode(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	err := ardaerrors.New(code, msg).WithRequestID(r.Header.Get("X-Request-Id"))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ardaerrors.Response{Error: *err})
}

// extractIP extracts the client IP from request headers or remote address.
func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip, _, _ := strings.Cut(fwd, ",")
		return normalizeIP(strings.TrimSpace(ip))
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return normalizeIP(host)
	}
	return normalizeIP(r.RemoteAddr)
}

func normalizeIP(value string) string {
	return strings.Trim(value, "[]")
}
