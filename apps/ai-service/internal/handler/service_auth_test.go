package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

func TestServiceAuthAcceptsGatewayAndRuntimeSources(t *testing.T) {
	secret := "unit-test-secret-0123456789abcdef"
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, source := range []string{"auth-gateway", "ai-runtime"} {
		token, err := identity.Issue(secret, source, "ai-service", time.Now(), time.Minute)
		if err != nil {
			t.Fatalf("issue token for %s: %v", source, err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", nil)
		req.Header.Set(identity.MetadataKey, token)
		rec := httptest.NewRecorder()
		ServiceAuthMiddleware(next, secret, true).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("source %s rejected with status %d", source, rec.Code)
		}
	}

	foreign, err := identity.Issue(secret, "unknown-service", "ai-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", nil)
	req.Header.Set(identity.MetadataKey, foreign)
	rec := httptest.NewRecorder()
	ServiceAuthMiddleware(next, secret, true).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign source accepted with status %d", rec.Code)
	}
}
