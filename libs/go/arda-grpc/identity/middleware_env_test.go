package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serveFromEnv(t *testing.T, sign func(*http.Request)) (int, bool) {
	t.Helper()
	called := false
	handler := RequireServiceAuthFromEnv("capital-service", "ai-service", "statistical-service")(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)
	req := httptest.NewRequest("GET", "/internal/reporting/capital-contracts", nil)
	if sign != nil {
		sign(req)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, called
}

func signAs(t *testing.T, source string) func(*http.Request) {
	return func(req *http.Request) {
		if err := SignRequest(req, testSecret, source, "capital-service", time.Now(), time.Minute); err != nil {
			t.Fatalf("sign: %v", err)
		}
	}
}

func TestRequireServiceAuthFromEnv_AllowsEveryListedCaller(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", testSecret)
	for _, source := range []string{"ai-service", "statistical-service"} {
		if code, called := serveFromEnv(t, signAs(t, source)); code != http.StatusOK || !called {
			t.Fatalf("%s: code=%d called=%t, want 200/true", source, code, called)
		}
	}
}

func TestRequireServiceAuthFromEnv_RejectsUnlistedAndUnsignedCallers(t *testing.T) {
	t.Setenv("ARDA_SERVICE_AUTH_SECRET", testSecret)
	if code, called := serveFromEnv(t, signAs(t, "loan-service")); code != http.StatusForbidden || called {
		t.Fatalf("unlisted caller: code=%d called=%t, want 403/false", code, called)
	}
	if code, called := serveFromEnv(t, nil); code != http.StatusUnauthorized || called {
		t.Fatalf("unsigned caller: code=%d called=%t, want 401/false", code, called)
	}
}

func TestRequireServiceAuthFromEnv_FailsClosedWithoutSecret(t *testing.T) {
	for name, value := range map[string]string{"missing": "", "too short": "short-secret"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ARDA_SERVICE_AUTH_SECRET", value)
			if code, called := serveFromEnv(t, signAs(t, "ai-service")); code != http.StatusServiceUnavailable || called {
				t.Fatalf("code=%d called=%t, want 503/false", code, called)
			}
		})
	}
}
