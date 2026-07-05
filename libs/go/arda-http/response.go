package ardahttp

import (
	"encoding/json"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"

	"github.com/google/uuid"
)

const HeaderRequestID = "X-Request-Id"
const HeaderTraceID = "X-Trace-Id"
const HeaderTraceParent = "traceparent"

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

// TraceID returns W3C trace id from X-Trace-Id or traceparent.
func TraceID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if id := strings.TrimSpace(r.Header.Get(HeaderTraceID)); id != "" {
		return id
	}
	tp := strings.TrimSpace(r.Header.Get(HeaderTraceParent))
	if tp == "" {
		return ""
	}
	parts := strings.Split(tp, "-")
	if len(parts) >= 2 && len(parts[1]) == 32 {
		return parts[1]
	}
	return ""
}

// SetCorrelationHeaders echoes request id on the response.
func SetCorrelationHeaders(w http.ResponseWriter, requestID string) {
	if requestID == "" {
		return
	}
	w.Header().Set(HeaderRequestID, requestID)
}

// SetRequestCorrelation echoes request and trace ids from the incoming request.
func SetRequestCorrelation(w http.ResponseWriter, r *http.Request) {
	requestID := RequestID(r)
	SetCorrelationHeaders(w, requestID)
	if traceID := TraceID(r); traceID != "" {
		w.Header().Set(HeaderTraceID, traceID)
	}
}

// WriteJSON writes a JSON response with correlation headers and response meta.
func WriteJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	SetRequestCorrelation(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(withResponseMeta(r, data))
}

// WriteAppError writes an arda-errors envelope with request_id.
func WriteAppError(w http.ResponseWriter, r *http.Request, status int, err *ardaerrors.Error) {
	if err == nil {
		err = ardaerrors.FromStatus(status, "")
	}
	if err.RequestID == "" {
		err = err.WithRequestID(RequestID(r))
	}
	SetRequestCorrelation(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ardaerrors.Response{Error: *err})
}

// WriteErrorCode writes a typed error code without a pre-built Error value.
func WriteErrorCode(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteAppError(w, r, status, ardaerrors.New(code, message))
}
