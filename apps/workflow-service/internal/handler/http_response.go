package handler

import (
	"net/http"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	ardahttp.WriteSuccess(w, r, status, v)
}

// writeAPIError renders a handler-decided status with canonical code
// resolution owned by arda-http.
func writeAPIError(w http.ResponseWriter, r *http.Request, status int, message string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(ardahttp.DeriveErrorCode(status, message), message))
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}

// writeListAll paginates an in-memory slice with the shared framing helper.
func writeListAll[T any](w http.ResponseWriter, r *http.Request, items []T) {
	paged, total, page, perPage := ardahttp.PageSlice(items, ardahttp.ParseListQuery(r.URL.Query()))
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, paged))
}

// writeListAny mirrors the unpaginated envelope for surfaces whose item type
// is only known as `any`; the shared helper derives totals without per-domain
// reflection switches.
func writeListAny(w http.ResponseWriter, r *http.Request, items any) {
	ardahttp.WriteEnvelopeAnyList(w, r, items)
}
