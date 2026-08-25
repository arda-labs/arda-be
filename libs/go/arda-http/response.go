package ardahttp

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"

	"github.com/google/uuid"
)

const HeaderRequestID = "X-Request-Id"
const HeaderTraceID = "X-Trace-Id"
const HeaderTraceParent = "traceparent"

// SuccessEnvelope is the canonical shape for migrated JSON endpoints. A
// non-migrated endpoint is an explicitly owned legacy/protocol surface; new
// handlers and consumers must not guess between response shapes at runtime.
type SuccessEnvelope[T any] struct {
	Result   T              `json:"result"`
	Success  bool           `json:"success"`
	Errors   []ProblemError `json:"errors"`
	Messages []string       `json:"messages"`
	Meta     ResponseMeta   `json:"meta"`
}

type ProblemError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	Path    string `json:"path,omitempty"`
}

// Problem is the canonical machine-readable error shape for migrated
// endpoints. It is deliberately flat so generic clients can parse it without
// knowing a service-specific wrapper.
type Problem struct {
	Type      string         `json:"type,omitempty"`
	Title     string         `json:"title,omitempty"`
	Status    int            `json:"status"`
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Errors    []ProblemError `json:"errors,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
}

// RequestID returns the correlation id from the request or generates one.
func RequestID(r *http.Request) string {
	if r == nil {
		return uuid.NewString()
	}
	for _, key := range []string{HeaderRequestID, "X-Correlation-Id", "Request-Id"} {
		if id := strings.TrimSpace(r.Header.Get(key)); validCorrelationValue(id) {
			return id
		}
	}
	// Persist the generated ID on the request so every helper invoked during
	// this request observes the same correlation value.
	id := uuid.NewString()
	r.Header.Set(HeaderRequestID, id)
	return id
}

func validCorrelationValue(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
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
	if !validTraceParent(tp) {
		return ""
	}
	parts := strings.Split(tp, "-")
	return parts[1]
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

// WriteSuccess writes the canonical success envelope for a migrated endpoint.
func WriteSuccess[T any](w http.ResponseWriter, r *http.Request, status int, result T, messages ...string) {
	SetRequestCorrelation(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if messages == nil {
		messages = []string{}
	}
	_ = json.NewEncoder(w).Encode(SuccessEnvelope[T]{
		Result:   result,
		Success:  true,
		Errors:   []ProblemError{},
		Messages: messages,
		Meta:     NewResponseMeta(r),
	})
}

// WriteAppError writes an arda-errors envelope with request_id.
func WriteAppError(w http.ResponseWriter, r *http.Request, status int, err *ardaerrors.Error) {
	if err == nil {
		err = ardaerrors.FromStatus(status, "")
	}
	// The request boundary owns correlation. Never echo an upstream/provider
	// request ID here, otherwise the response header and body can point to
	// different traces.
	err = err.WithRequestID(RequestID(r))
	SetRequestCorrelation(w, r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ardaerrors.Response{Error: *err})
}

// WriteErrorCode writes a typed error code without a pre-built Error value.
func WriteErrorCode(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteAppError(w, r, status, ardaerrors.New(code, message))
}

// WriteProblem writes the canonical RFC-style error body for migrated
// endpoints. Fields are represented as validation errors while preserving the
// legacy error code/message semantics.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, err *ardaerrors.Error) {
	if err == nil {
		err = ardaerrors.FromStatus(status, "")
	}
	requestID := RequestID(r)
	problem := Problem{
		Type:      "https://docs.arda.io.vn/problems/" + err.Code,
		Title:     http.StatusText(status),
		Status:    status,
		Code:      err.Code,
		Message:   err.Message,
		RequestID: requestID,
		TraceID:   TraceID(r),
	}
	for field, code := range err.Fields {
		problem.Errors = append(problem.Errors, ProblemError{Field: field, Code: code, Message: code})
	}
	SetRequestCorrelation(w, r)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}
