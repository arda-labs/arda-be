package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/handler"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

const internalAITestSecret = "01234567890123456789012345678901"

func TestInternalAIService_RequiresAIServiceToken(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", internalAITestSecret)

	next := internalAIService(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	valid, err := identity.Issue(internalAITestSecret, "ai-service", "platform-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue valid token: %v", err)
	}

	tests := []struct {
		name   string
		token  string
		status int
	}{
		{name: "missing token", status: http.StatusUnauthorized},
		{name: "valid ai-service token", token: valid, status: http.StatusNoContent},
		{name: "wrong source", token: func() string {
			t, _ := identity.Issue(internalAITestSecret, "crm-service", "platform-service", time.Now(), time.Minute)
			return t
		}(), status: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/ai/organizations", nil)
			if tt.token != "" {
				req.Header.Set("X-Service-Auth", tt.token)
			}
			res := httptest.NewRecorder()
			next.ServeHTTP(res, req)
			if res.Code != tt.status {
				t.Errorf("status = %d, want %d", res.Code, tt.status)
			}
		})
	}
}

func TestInternalAIRoutes_ReachHandlerOnlyWhenSigned(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", internalAITestSecret)
	router := NewRouter(
		handler.NewPlatformHandler(nil, nil),
		handler.NewCalendarHandler(nil),
		nil,
		nil,
	)

	unsigned := httptest.NewRequest(http.MethodGet, "/internal/ai/organizations", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, unsigned)
	// Missing token → 401 from the middleware, never a leak from the handler.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without x-service-auth, got %d", rec.Code)
	}

	wrongSource := httptest.NewRequest(http.MethodGet, "/internal/ai/organizations", nil)
	wrongSource.Header.Set("X-Tenant-Id", "tenant-1")
	if err := identity.SignRequest(wrongSource, internalAITestSecret, "crm-service", "platform-service", time.Now(), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, wrongSource)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-ai-service caller, got %d", rec.Code)
	}

	signed := httptest.NewRequest(http.MethodGet, "/internal/ai/organizations?per_page=0", nil)
	signed.Header.Set("X-Tenant-Id", "tenant-1")
	if err := identity.SignRequest(signed, internalAITestSecret, "ai-service", "platform-service", time.Now(), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec = httptest.NewRecorder()
	// The handler rejects the request before any store call when per_page is
	// invalid — reaching that response proves the middleware passed the
	// signed caller.
	router.ServeHTTP(rec, signed)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 from handler for invalid per_page, got %d", rec.Code)
	}
}
