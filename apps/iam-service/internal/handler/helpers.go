package handler

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func respondJSON(w http.ResponseWriter, status int, data any) {
	respondJSONWithRequest(w, nil, status, data)
}

func respondJSONWithRequest(w http.ResponseWriter, r *http.Request, status int, data any) {
	ardahttp.WriteJSON(w, r, status, data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondRequestError(w, nil, status, ardaerrors.CodeForStatus(status), msg)
}

func respondErrorCode(w http.ResponseWriter, status int, code, msg string) {
	respondRequestError(w, nil, status, code, msg)
}

func respondRequestError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	ardahttp.WriteErrorCode(w, r, status, code, msg)
}

func respondRequestErrorCode(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	respondRequestError(w, r, status, code, msg)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseAdminListQuery(r *http.Request) (page, perPage int, search string) {
	listQuery := ardahttp.ParseListQuery(r.URL.Query())
	page = listQuery.Page
	perPage = listQuery.PerPage
	if raw := r.URL.Query().Get("size"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			perPage = n
		}
	}
	if perPage > 100 {
		perPage = 100
	}
	search = firstNonEmpty(listQuery.Q, r.URL.Query().Get("search"))
	return page, perPage, search
}

func respondAdminList(w http.ResponseWriter, r *http.Request, legacyKey string, items any, total, page, perPage int) {
	totalPages := 0
	if perPage > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	respondJSONWithRequest(w, r, http.StatusOK, map[string]any{
		"items":      items,
		legacyKey:    items,
		"total":      total,
		"page":       page,
		"per_page":   perPage,
		"size":       perPage,
		"totalPages": totalPages,
	})
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
