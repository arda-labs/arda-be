package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/hrm-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const testSecret = "01234567890123456789012345678901"

func TestInternalAIEmployees_RejectsUnsignedCallers(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", testSecret)
	router := NewRouter(handler.NewHRMHandler(nil, nil))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/internal/ai/employees", nil))
	// Fail-closed: the unsigned request is rejected before reaching the
	// handler (4xx from either the tenant-scope guard or the service-auth
	// middleware — both deny the caller).
	if rec.Code < 400 || rec.Code >= 500 {
		t.Errorf("expected 4xx without x-service-auth, got %d", rec.Code)
	}

	// Signed but from a non-ai-service source → forbidden.
	wrong := httptest.NewRequest(http.MethodGet, "/internal/ai/employees", nil)
	wrong.Header.Set("X-Auth-Checked", "true")
	wrong.Header.Set("X-Tenant-Id", "tenant-1")
	if err := identity.SignRequest(wrong, testSecret, "crm-service", "hrm-service", time.Now(), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, wrong)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-ai-service caller, got %d", rec.Code)
	}
}

func TestInternalAIEmployees_SignedCallerReachesHandler(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", testSecret)
	router := NewRouter(handler.NewHRMHandler(nil, nil))

	signed := httptest.NewRequest(http.MethodDelete, "/internal/ai/employees", nil)
	signed.Header.Set("X-Auth-Checked", "true")
	signed.Header.Set("X-Tenant-Id", "tenant-1")
	if err := identity.SignRequest(signed, testSecret, "ai-service", "hrm-service", time.Now(), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec := httptest.NewRecorder()
	// The handler rejects the wrong method (405) before touching the store —
	// reaching that response proves the middleware passed the signed caller.
	router.ServeHTTP(rec, signed)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 from handler for signed ai-service caller, got %d", rec.Code)
	}
}
