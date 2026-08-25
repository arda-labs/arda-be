package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

func TestInternalServiceRequiresAuthGatewayAssertion(t *testing.T) {
	const secret = "01234567890123456789012345678901"
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", secret)

	next := internalService(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	valid, err := identity.Issue(secret, "auth-gateway", "iam-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue valid assertion: %v", err)
	}

	tests := []struct {
		name   string
		token  string
		status int
	}{
		{name: "missing", status: http.StatusUnauthorized},
		{name: "valid", token: valid, status: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/iam/users/by-id/u/context", nil)
			if tt.token != "" {
				req.Header.Set("X-Service-Auth", tt.token)
			}
			res := httptest.NewRecorder()
			next.ServeHTTP(res, req)
			if res.Code != tt.status {
				t.Fatalf("status = %d, want %d", res.Code, tt.status)
			}
		})
	}
}

func TestInternalServiceRejectsOtherWorkloads(t *testing.T) {
	const secret = "01234567890123456789012345678901"
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", secret)

	token, err := identity.Issue(secret, "workflow-service", "iam-service", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue assertion: %v", err)
	}
	next := internalService(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/internal/iam/users/by-id/u/context", nil)
	req.Header.Set("X-Service-Auth", token)
	res := httptest.NewRecorder()
	next.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}
