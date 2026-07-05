package handler

import (
	"net"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func respondJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	ardahttp.WriteJSON(w, r, status, data)
}

func respondJSONWithRequest(w http.ResponseWriter, r *http.Request, status int, data any) {
	ardahttp.WriteJSON(w, r, status, data)
}

func respondError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	respondRequestError(w, r, status, errorCodeFor(status, msg), msg)
}

func respondErrorCode(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	respondRequestError(w, r, status, code, msg)
}

func respondRequestError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	ardahttp.WriteErrorCode(w, r, status, code, msg)
}

func respondRequestErrorCode(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	respondRequestError(w, r, status, code, msg)
}

func errorCodeFor(status int, msg string) string {
	code := ardaerrors.CodeForStatus(status)
	lower := strings.ToLower(msg)
	switch {
	case status == http.StatusBadRequest && strings.Contains(lower, "json"):
		return ardaerrors.CodeInvalidJSON
	case status == http.StatusBadRequest && strings.Contains(lower, "required"):
		return ardaerrors.CodeRequired
	case status == http.StatusNotFound:
		return ardaerrors.CodeNotFound
	case status == http.StatusConflict:
		return ardaerrors.CodeConflict
	case status == http.StatusUnauthorized:
		return ardaerrors.CodeUnauthorized
	case status == http.StatusForbidden:
		return ardaerrors.CodeForbidden
	default:
		return code
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseAdminListQuery(r *http.Request) ardahttp.ListQuery {
	return ardahttp.ParseListQuery(r.URL.Query())
}

func listSortOrder(listQuery ardahttp.ListQuery) string {
	if strings.EqualFold(listQuery.Order, "asc") {
		return "ASC"
	}
	return "DESC"
}

func respondAdminList[T any](w http.ResponseWriter, r *http.Request, items []T, total, page, perPage int) {
	ardahttp.WriteList(w, r, page, perPage, total, items)
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
