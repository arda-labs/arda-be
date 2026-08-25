package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeResult(w http.ResponseWriter, r *http.Request, value any, err error) {
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorCode(w, r, http.StatusNotFound, ardaerrors.CodeNotFound, "not found")
			return
		}
		writeErrorCode(w, r, http.StatusInternalServerError, ardaerrors.CodeInternal, err.Error())
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, value)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	writeErrorCode(w, r, status, ardaerrors.CodeForStatus(status), message)
}

func writeErrorCode(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}

func writeListAll[T any](w http.ResponseWriter, r *http.Request, items []T) {
	if items == nil {
		items = []T{}
	}
	query := ardahttp.ParseListQuery(r.URL.Query())
	page, perPage := query.Page, query.PerPage
	total := len(items)
	if query.All {
		perPage = maxInt(total, 1)
		page = 1
	} else {
		start := query.Offset()
		if start >= total {
			items = []T{}
		} else {
			end := start + perPage
			if end > total {
				end = total
			}
			items = items[start:end]
		}
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, items))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return false
	}
	return true
}
