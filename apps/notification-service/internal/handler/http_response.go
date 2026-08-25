package handler

import (
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	ardahttp.WriteSuccess(w, r, status, v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	code := ardaerrors.CodeForStatus(status)
	if status == http.StatusBadRequest && strings.Contains(strings.ToLower(message), "json") {
		code = ardaerrors.CodeInvalidJSON
	}
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}
