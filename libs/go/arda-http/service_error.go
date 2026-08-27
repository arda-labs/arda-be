package ardahttp

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// DeriveErrorCode resolves the canonical problem code for a handler-decided
// HTTP status plus raw message. It centralises the invalid-json / required
// message heuristics that previously lived separately in every service and
// maps upstream availability failures (502/503/504) to BadGateway so a
// dependency outage never surfaces as internal.
func DeriveErrorCode(status int, message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case status == http.StatusBadRequest && strings.Contains(lower, "json"):
		return ardaerrors.CodeInvalidJSON
	case status == http.StatusBadRequest && strings.Contains(lower, "required"):
		return ardaerrors.CodeRequired
	case status == http.StatusBadGateway,
		status == http.StatusServiceUnavailable,
		status == http.StatusGatewayTimeout:
		return ardaerrors.CodeBadGateway
	default:
		return ardaerrors.CodeForStatus(status)
	}
}

// DeriveStatusFromMessage is the transitional shim for service layers that
// still return plain errors. Constructors should migrate toward wrapping
// causes with ardaerrors.Wrap so handlers can stop inspecting text; keyword
// matching here preserves the historical mapping of existing messages only.
func DeriveStatusFromMessage(message string) int {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "not found"):
		return http.StatusNotFound
	case strings.Contains(lower, "conflict"),
		strings.Contains(lower, "cannot be"),
		strings.Contains(lower, "must be"):
		return http.StatusConflict
	case strings.Contains(lower, "workflow"),
		strings.Contains(lower, "upstream"):
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// WriteServiceError renders a service-layer error as a canonical Problem.
//
// Resolution order:
//  1. typed *ardaerrors.Error → StatusForCode(code);
//  2. sql.ErrNoRows → 404 common.error.not_found;
//  3. legacy free-form errors → DeriveStatusFromMessage heuristics.
//
// A nil error is a no-op.
func WriteServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	var typed *ardaerrors.Error
	if errors.As(err, &typed) {
		if typed != nil {
			WriteProblem(w, r, ardaerrors.StatusForCode(typed.Code), typed.WithRequestID(RequestID(r)))
			return
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		WriteProblem(w, r, http.StatusNotFound, ardaerrors.New(ardaerrors.CodeNotFound, "resource not found"))
		return
	}
	message := err.Error()
	status := DeriveStatusFromMessage(message)
	WriteProblem(w, r, status, ardaerrors.New(DeriveErrorCode(status, message), message))
}