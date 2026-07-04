package ardahttp

import (
	"encoding/json"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"

	"github.com/google/uuid"
)

const HeaderRequestID = "X-Request-Id"

// RequestID returns the correlation id from the request or generates one.
func RequestID(r *http.Request) string {
	if r == nil {
		return uuid.NewString()
	}
	for _, key := range []string{HeaderRequestID, "X-Correlation-Id", "Request-Id"} {
		if id := strings.TrimSpace(r.Header.Get(key)); id != "" {
			return id
		}
	}
	return uuid.NewString()
}

// SetCorrelationHeaders echoes request id on the response.
func SetCorrelationHeaders(w http.ResponseWriter, requestID string) {
	if requestID == "" {
		return
	}
	w.Header().Set(HeaderRequestID, requestID)
}

// WriteJSON writes a JSON response with correlation headers.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	requestID := RequestID(r)
	SetCorrelationHeaders(w, requestID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// WriteAppError writes an arda-errors envelope with request_id.
func WriteAppError(w http.ResponseWriter, r *http.Request, status int, err *ardaerrors.Error) {
	if err == nil {
		err = ardaerrors.FromStatus(status, "")
	}
	if err.RequestID == "" {
		err = err.WithRequestID(RequestID(r))
	}
	SetCorrelationHeaders(w, err.RequestID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ardaerrors.Response{Error: *err})
}

// WriteErrorCode writes a typed error code without a pre-built Error value.
func WriteErrorCode(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteAppError(w, r, status, ardaerrors.New(code, message))
}
