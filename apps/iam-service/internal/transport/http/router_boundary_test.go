package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIAMDoesNotExposeBrowserAuthBoundary(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login-page?login_challenge=legacy", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("IAM browser auth route status = %d, want %d", resp.Code, http.StatusNotFound)
	}
}
