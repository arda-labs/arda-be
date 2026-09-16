package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
)

func TestPublicBrandingPayloadStripsAuthSettings(t *testing.T) {
	payload := publicBrandingPayload(`{"appName":"Bank","loginLogoUrl":"/logo.svg","maxFailedAttempts":9}`)

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if got["appName"] != "Bank" || got["loginLogoUrl"] != "/logo.svg" {
		t.Fatalf("missing branding fields: %#v", got)
	}
	if _, ok := got["maxFailedAttempts"]; ok {
		t.Fatalf("leaked auth setting: %#v", got)
	}
}

// A targeted delete/update that matched no row must answer 404, not {"ok": true}.
func TestWriteServiceErrorMapsNotFoundTo404(t *testing.T) {
	for _, err := range []error{
		repository.ErrNotFound,
		fmt.Errorf("delete organization: %w", repository.ErrNotFound),
	} {
		req := httptest.NewRequest(http.MethodDelete, "/api/platform/organizations/unknown", nil)
		rec := httptest.NewRecorder()
		writeServiceError(rec, req, err)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for %v", rec.Code, err)
		}
	}
}

func TestWriteServiceErrorKeepsInternalErrorsAs500(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/platform/organizations/x", nil)
	rec := httptest.NewRecorder()
	writeServiceError(rec, req, fmt.Errorf("connection reset"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
