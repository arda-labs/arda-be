package handler

import (
	"net/http"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
)

func respondJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	ardahttp.WriteJSON(w, r, status, data)
}

func respondError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	code := ardaerrors.CodeForStatus(status)
	if status == http.StatusBadRequest && msg == "invalid body" {
		code = ardaerrors.CodeInvalidJSON
	}
	ardahttp.WriteErrorCode(w, r, status, code, msg)
}

func respondPaged(w http.ResponseWriter, r *http.Request, txns []domain.Transaction, total, page, perPage int) {
	ardahttp.WriteList(w, r, page, perPage, total, txns)
}

func respondUnpagedList(w http.ResponseWriter, r *http.Request, items any) {
	respondJSON(w, r, http.StatusOK, map[string]any{"items": items})
}

func respondList(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondUnpagedList(w, r, data)
}
