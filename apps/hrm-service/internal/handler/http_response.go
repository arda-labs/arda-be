package handler

import (
	"encoding/json"
	"net/http"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeResult(w http.ResponseWriter, r *http.Request, value any, err error) {
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	ardahttp.WriteSuccess(w, r, http.StatusOK, value)
}

// writeServiceError maps service-layer errors through the shared resolver;
// sql.ErrNoRows keeps its 404 mapping there.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	ardahttp.WriteServiceError(w, r, err)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	writeErrorCode(w, r, status, ardahttp.DeriveErrorCode(status, message), message)
}

func writeErrorCode(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}

// writeListAll paginates an in-memory slice with the shared framing helper.
func writeListAll[T any](w http.ResponseWriter, r *http.Request, items []T) {
	paged, total, page, perPage := ardahttp.PageSlice(items, ardahttp.ParseListQuery(r.URL.Query()))
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, paged))
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErrorCode(w, r, http.StatusBadRequest, ardaerrors.CodeInvalidJSON, "invalid json")
		return false
	}
	return true
}
