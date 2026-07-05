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

func respondPaged(w http.ResponseWriter, r *http.Request, txns []domain.Transaction, total, page, size int) {
	if txns == nil {
		txns = []domain.Transaction{}
	}
	totalPages := 0
	if size > 0 {
		totalPages = (total + size - 1) / size
	}
	// Standard list shape + legacy keys during migration.
	ardahttp.WriteJSON(w, r, http.StatusOK, map[string]any{
		"items":        txns,
		"transactions": txns,
		"total":        total,
		"page":         page,
		"per_page":     size,
		"size":         size,
		"totalPages":   totalPages,
	})
}

func respondList(w http.ResponseWriter, r *http.Request, key string, data any, err error) {
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"items": data,
		key:     data,
	})
}
