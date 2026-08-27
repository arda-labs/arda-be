package handler

import (
	"net/http"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	ardahttp.WriteSuccess(w, r, status, value)
}

// writeError renders a handler-decided status with canonical code resolution
// owned by arda-http instead of local keyword switches.
func writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(ardahttp.DeriveErrorCode(status, message), message))
}

// writeServiceError maps service-layer errors through the shared resolver:
// typed *ardaerrors.Error values win, then sql.ErrNoRows, then the documented
// legacy message heuristics.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	ardahttp.WriteServiceError(w, r, err)
}

func writeErrorCode(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, message))
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}

// writeListAll paginates an in-memory slice with the shared framing helper so
// all/services produce identical envelope math (all=1, tree/options views,
// offset clamping).
func writeListAll[T any](w http.ResponseWriter, r *http.Request, items []T) {
	paged, total, page, perPage := ardahttp.PageSlice(items, ardahttp.ParseListQuery(r.URL.Query()))
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, paged))
}
