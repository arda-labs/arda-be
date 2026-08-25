package handler

import (
	"log/slog"
	"net/http"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	ardahttp.WriteSuccess(w, r, status, payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	slog.Warn("returning client error", "status", status, "code", code, "message", message)
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}
