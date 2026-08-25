package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExchangeCodeDoesNotAcceptDevShortcut(t *testing.T) {
	h := &BFFHandler{}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/callback", strings.NewReader(`{"code":"dev"}`))
	rec := httptest.NewRecorder()

	h.ExchangeCode(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if strings.Contains(rec.Body.String(), "dev-user") {
		t.Fatalf("dev session shortcut still exposed: %s", rec.Body.String())
	}
}
