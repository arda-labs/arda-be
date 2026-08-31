package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
)

func TestInternalAIService_RequiresAIServiceToken(t *testing.T) {
	const secret = "01234567890123456789012345678901"
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", secret)

	next := internalAIService(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	valid, err := identity.Issue(secret, "ai-service", "iam-service", time.Now(), time.Minute)
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
			t, _ := identity.Issue(secret, "auth-gateway", "iam-service", time.Now(), time.Minute)
			return t
		}(), status: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/ai/users", nil)
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
