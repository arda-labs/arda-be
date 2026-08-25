package handler

import (
	"net/http"
	"reflect"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	ardahttp.WriteSuccess(w, r, status, v)
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
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
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
		ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(1, maxInt(total, 1), total, items))
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
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, items))
}

func writeListAny(w http.ResponseWriter, r *http.Request, items any) {
	if items == nil {
		items = []any{}
	}
	total := 0
	value := reflect.ValueOf(items)
	if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Array) {
		total = value.Len()
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"items":    items,
		"page":     1,
		"per_page": maxInt(total, 1),
		"total":    total,
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
