package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	ardahttp.WriteSuccess(w, r, status, value)
}

func writeListAll[T any](w http.ResponseWriter, r *http.Request, items []T) {
	if items == nil {
		items = []T{}
	}
	listQuery := ardahttp.ParseListQuery(r.URL.Query())
	page := listQuery.Page
	perPage := listQuery.PerPage
	total := len(items)

	if listQuery.All || listQuery.View != "" {
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

func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	code := ardaerrors.CodeForStatus(status)
	if status == http.StatusBadRequest && strings.Contains(strings.ToLower(message), "json") {
		code = ardaerrors.CodeInvalidJSON
	}
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, sql.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "not found") {
		writeError(w, r, http.StatusNotFound, err.Error())
		return
	}
	if strings.Contains(strings.ToLower(err.Error()), "conflict") ||
		strings.Contains(strings.ToLower(err.Error()), "cannot be") ||
		strings.Contains(strings.ToLower(err.Error()), "must be") {
		writeError(w, r, http.StatusConflict, err.Error())
		return
	}
	if strings.Contains(strings.ToLower(err.Error()), "workflow") {
		writeError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	writeError(w, r, http.StatusInternalServerError, err.Error())
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}

func writeErrorCode(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
