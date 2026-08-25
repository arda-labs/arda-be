package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthDeniedUsesCanonicalProblem(t *testing.T) {
	h := &AuthHandler{}
	req := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	req.Header.Set("X-Request-Id", "req-auth-denied")
	rec := httptest.NewRecorder()

	h.respondDenied(rec, req, "missing authorization")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type = %q", got)
	}
	var problem struct {
		Code      string `json:"code"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "auth.error.unauthorized" || problem.RequestID != "req-auth-denied" {
		t.Fatalf("problem = %#v", problem)
	}
}
