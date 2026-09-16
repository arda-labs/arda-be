package ardahttp

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitRequestBodyRejectsOversizedBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LimitRequestBody(w, r, 8)
		_, err := io.ReadAll(r.Body)
		var maxErr *http.MaxBytesError
		if !errors.As(err, &maxErr) {
			t.Fatalf("read error = %v, want *http.MaxBytesError", err)
		}
		if maxErr.Limit != 8 {
			t.Fatalf("limit = %d, want 8", maxErr.Limit)
		}
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789")))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestLimitRequestBodyAllowsBodyWithinLimit(t *testing.T) {
	var body string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LimitRequestBody(w, r, 8)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read within limit: %v", err)
		}
		body = string(raw)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345678")))
	if body != "12345678" {
		t.Fatalf("body = %q, want %q", body, "12345678")
	}
}

func TestLimitRequestBodyNonPositiveLimitIsNoop(t *testing.T) {
	var body string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		LimitRequestBody(w, r, 0)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read without limit: %v", err)
		}
		body = string(raw)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("unbounded")))
	if body != "unbounded" {
		t.Fatalf("body = %q, want %q", body, "unbounded")
	}
}

func TestLimitBodyMiddlewareAppliesLimit(t *testing.T) {
	handler := LimitBodyMiddleware(4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err == nil {
			t.Fatal("expected middleware to enforce the body limit")
		}
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345")))
}
