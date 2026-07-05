package handler

import (
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	ardahttp.WriteJSON(w, r, status, v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	code := ardaerrors.CodeForStatus(status)
	if status == http.StatusBadRequest && strings.Contains(strings.ToLower(message), "json") {
		code = ardaerrors.CodeInvalidJSON
	}
	ardahttp.WriteErrorCode(w, r, status, code, message)
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
