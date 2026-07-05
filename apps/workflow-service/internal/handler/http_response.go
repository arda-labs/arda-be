package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	ardahttp.WriteJSON(w, r, status, v)
}

func writeAPIError(w http.ResponseWriter, r *http.Request, status int, message string) {
	code := ardaerrors.CodeForStatus(status)
	lower := strings.ToLower(message)
	switch {
	case status == http.StatusBadRequest && strings.Contains(lower, "json"):
		code = ardaerrors.CodeInvalidJSON
	case status == http.StatusBadRequest && strings.Contains(lower, "required"):
		code = ardaerrors.CodeRequired
	case status == http.StatusUnauthorized && strings.Contains(lower, "missing x-user-id"):
		code = ardaerrors.CodeUnauthorized
	case status == http.StatusServiceUnavailable:
		code = ardaerrors.CodeBadGateway
	}
	ardahttp.WriteErrorCode(w, r, status, code, message)
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteErrorCode(w, r, http.StatusMethodNotAllowed, ardaerrors.CodeMethodNotAllowed, "method not allowed")
}

func writeListAll[T any](w http.ResponseWriter, r *http.Request, items []T) {
	if items == nil {
		items = []T{}
	}
	listQuery := ardahttp.ParseListQuery(r.URL.Query())
	page := listQuery.Page
	perPage := listQuery.PerPage
	total := len(items)
	if listQuery.All {
		ardahttp.WriteList(w, r, 1, maxInt(total, 1), total, items)
		return
	}
	start := listQuery.Offset()
	if start >= total {
		items = []T{}
	} else {
		end := start + perPage
		if end > total {
			end = total
		}
		items = items[start:end]
	}
	ardahttp.WriteList(w, r, page, perPage, total, items)
}

func writeRawJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	ardahttp.SetRequestCorrelation(w, r)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
