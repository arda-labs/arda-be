package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

func TestServiceAuthAcceptsGatewayOnly(t *testing.T) {
	secret := "unit-test-secret-0123456789abcdef"
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	token, err := identity.Issue(secret, "auth-gateway", "ai-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue token for auth-gateway: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", nil)
	req.Header.Set(identity.MetadataKey, token)
	rec := httptest.NewRecorder()
	ServiceAuthMiddleware(next, secret, true).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("source auth-gateway rejected with status %d", rec.Code)
	}

	// The retired ai-runtime adapter must no longer be a trusted source.
	retired, err := identity.Issue(secret, "ai-runtime", "ai-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/ai/agent", nil)
	req.Header.Set(identity.MetadataKey, retired)
	rec = httptest.NewRecorder()
	ServiceAuthMiddleware(next, secret, true).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("retired source ai-runtime accepted with status %d", rec.Code)
	}

	foreign, err := identity.Issue(secret, "unknown-service", "ai-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/ai/agent", nil)
	req.Header.Set(identity.MetadataKey, foreign)
	rec = httptest.NewRecorder()
	ServiceAuthMiddleware(next, secret, true).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign source accepted with status %d", rec.Code)
	}
}
