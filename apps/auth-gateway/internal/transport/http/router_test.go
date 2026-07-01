package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusRecorderCapturesStatusAndBytes(t *testing.T) {
	rec := httptest.NewRecorder()
	statusRec := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	statusRec.WriteHeader(http.StatusCreated)
	n, err := statusRec.Write([]byte("ok"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if statusRec.status != http.StatusCreated {
		t.Fatalf("status = %d, want %d", statusRec.status, http.StatusCreated)
	}
	if statusRec.bytes != n {
		t.Fatalf("bytes = %d, want %d", statusRec.bytes, n)
	}
}

func TestSlowRequestLoggerDetectsEventStreamAccept(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/notifications/stream", nil)
	req.Header.Set("Accept", "text/event-stream")

	if !isEventStreamRequest(req) {
		t.Fatal("event stream request was not detected")
	}
}
