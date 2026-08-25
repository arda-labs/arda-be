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
	if status == http.StatusBadRequest && msg == "invalid body" {
		code = ardaerrors.CodeInvalidJSON
	}
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, msg))
}

func respondPaged(w http.ResponseWriter, r *http.Request, txns []domain.Transaction, total, page, perPage int) {
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, txns))
}

func respondUnpagedList(w http.ResponseWriter, r *http.Request, items any) {
	total := lenItems(items)
	ardahttp.WriteSuccess(w, r, http.StatusOK, unpagedListResult{
		Items:   items,
		Page:    1,
		PerPage: maxInt(total, 1),
		Total:   total,
	})
}

type unpagedListResult struct {
	Items   any `json:"items"`
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

func lenItems(items any) int {
	switch value := items.(type) {
	case []domain.ProcessConfig:
		return len(value)
	case []domain.AccountClassification:
		return len(value)
	case []domain.JournalDefinition:
		return len(value)
	case []domain.NamedAccountMapping:
		return len(value)
	default:
		return 0
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func respondList(w http.ResponseWriter, r *http.Request, data any, err error) {
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	respondUnpagedList(w, r, data)
}
