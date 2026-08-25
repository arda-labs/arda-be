package ardahttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

func TestWriteSuccessUsesCanonicalEnvelope(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	req.Header.Set(HeaderRequestID, "req-1")
	res := httptest.NewRecorder()

	WriteSuccess(res, req, http.StatusOK, map[string]string{"id": "item-1"}, "loaded")

	var body SuccessEnvelope[map[string]string]
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Result["id"] != "item-1" || len(body.Errors) != 0 || body.Meta.RequestID != "req-1" {
		t.Fatalf("unexpected success envelope: %+v", body)
	}
}

func TestWriteProblemUsesCanonicalErrorAndCorrelation(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/items", nil)
	req.Header.Set(HeaderRequestID, "req-1")
	req.Header.Set(HeaderTraceID, "trace-1")
	res := httptest.NewRecorder()

	err := ardaerrors.New("validation.invalid_input", "invalid item")
	err.WithField("name", "required")
	WriteProblem(res, req, http.StatusUnprocessableEntity, err)

	var body Problem
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "validation.invalid_input" || body.RequestID != "req-1" || body.TraceID != "trace-1" || len(body.Errors) != 1 {
		t.Fatalf("unexpected problem: %+v", body)
	}
	if got := res.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("unexpected content type %q", got)
	}
}

func TestWriteProblemUsesRequestCorrelationOverUpstreamErrorID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set(HeaderRequestID, "edge-request")
	res := httptest.NewRecorder()

	err := ardaerrors.New("upstream.error", "upstream failed").WithRequestID("upstream-request")
	WriteProblem(res, req, http.StatusBadGateway, err)

	var body Problem
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if got := res.Header().Get(HeaderRequestID); got != "edge-request" {
		t.Fatalf("response request id = %q", got)
	}
	if body.RequestID != "edge-request" {
		t.Fatalf("body request id = %q", body.RequestID)
	}
}
