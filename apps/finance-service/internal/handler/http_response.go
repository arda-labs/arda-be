package handler

import (
	"net/http"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"

	"github.com/arda-labs/arda/apps/finance-service/internal/domain"
)

func respondJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	ardahttp.WriteSuccess(w, r, status, data)
}

func respondError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	code := ardaerrors.CodeForStatus(status)
	// Historical caller contract: bare "invalid body" carries no JSON keyword.
	if status == http.StatusBadRequest && msg == "invalid body" {
		code = ardaerrors.CodeInvalidJSON
	}
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, msg))
}

func respondPaged(w http.ResponseWriter, r *http.Request, txns []domain.Transaction, total, page, perPage int) {
	ardahttp.WriteEnvelopeList(w, r, http.StatusOK, page, perPage, total, txns)
}

// respondList replaces the previous `any` + per-domain lenItems type switch:
// generics give real totals for every slice, and service errors flow through
// the shared typed resolver instead of a hand-rolled 500.
func respondList[T any](w http.ResponseWriter, r *http.Request, items []T, err error) {
	if err != nil {
		ardahttp.WriteServiceError(w, r, err)
		return
	}
	ardahttp.WriteEnvelopeUnpaged(w, r, items)
}
