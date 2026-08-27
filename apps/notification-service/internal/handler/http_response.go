package handler

import (
	"net/http"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	ardahttp.WriteSuccess(w, r, status, v)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(ardahttp.DeriveErrorCode(status, message), message))
}
