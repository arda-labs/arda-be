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
	ardahttp.WriteJSON(w, r, http.StatusOK, value)
}

func writeListAll[T any](w http.ResponseWriter, r *http.Request, items []T) {
	if items == nil {
		items = []T{}
	}
	total := len(items)
	perPage := total
	if perPage == 0 {
		perPage = 1
	}
	ardahttp.WriteList(w, r, 1, perPage, total, items)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	writeErrorCode(w, r, status, ardaerrors.CodeForStatus(status), message)
}

func writeErrorCode(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	ardahttp.WriteErrorCode(w, r, status, code, message)
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return false
	}
	return true
}
