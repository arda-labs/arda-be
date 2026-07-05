package ardahttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithResponseMetaObject(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, "req-123")
	req.Header.Set(HeaderTraceID, "trace-456")

	out := withResponseMeta(req, map[string]string{"status": "ok"})
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["meta"]; !ok {
		t.Fatal("expected meta key")
	}
	if _, ok := body["status"]; !ok {
		t.Fatal("expected status key")
	}

	var meta ResponseMeta
	if err := json.Unmarshal(body["meta"], &meta); err != nil {
		t.Fatal(err)
	}
	if meta.RequestID != "req-123" {
		t.Fatalf("request_id = %q", meta.RequestID)
	}
	if meta.TraceID != "trace-456" {
		t.Fatalf("trace_id = %q", meta.TraceID)
	}
	if meta.Timestamp == "" {
		t.Fatal("expected timestamp")
	}
}

func TestWithResponseMetaList(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	out := withResponseMeta(req, NewListResponse(1, 20, 0, []string{}))
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"meta", "items", "page", "per_page", "total"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("expected %q key", key)
		}
	}
}
