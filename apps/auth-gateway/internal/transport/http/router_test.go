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

func TestCORSMiddlewareAllowsConfiguredCredentialedOrigin(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "https://arda.io.vn")
	req := httptest.NewRequest(http.MethodOptions, "/api/auth/me", nil)
	req.Header.Set("Origin", "https://arda.io.vn")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://arda.io.vn" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q", got)
	}
}

func TestCORSMiddlewareRejectsUnknownOrigin(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), "https://arda.io.vn")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
