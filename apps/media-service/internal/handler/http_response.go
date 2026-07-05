package handler

import (
	"log/slog"
	"net/http"

	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	ardahttp.WriteJSON(w, r, status, payload)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	slog.Warn("returning client error", "status", status, "code", code, "message", message)
	ardahttp.WriteErrorCode(w, r, status, code, message)
}
