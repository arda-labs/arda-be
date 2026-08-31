package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef" // 32 chars

func TestRequireServiceAuth_AllowsValidCaller(t *testing.T) {
	handler := RequireServiceAuth(testSecret, "crm-service", AllowedSources("ai-service"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/internal/ai/customers/1", nil)
	if err := SignRequest(req, testSecret, "ai-service", "crm-service", time.Now(), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRequireServiceAuth_MissingToken(t *testing.T) {
	handler := RequireServiceAuth(testSecret, "crm-service", AllowedSources("ai-service"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/internal/ai/customers/1", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing token, got %d", rec.Code)
	}
}

func TestRequireServiceAuth_WrongAudience(t *testing.T) {
	handler := RequireServiceAuth(testSecret, "crm-service", AllowedSources("ai-service"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	// Token signed for finance-service — crm-service must reject.
	req := httptest.NewRequest("GET", "/internal/ai/customers/1", nil)
	if err := SignRequest(req, testSecret, "ai-service", "finance-service", time.Now(), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong audience, got %d", rec.Code)
	}
}

func TestRequireServiceAuth_WrongSource(t *testing.T) {
	handler := RequireServiceAuth(testSecret, "crm-service", AllowedSources("ai-service"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	// Token from auth-gateway — not in the ai-service allowlist → 403.
	req := httptest.NewRequest("GET", "/internal/ai/customers/1", nil)
	if err := SignRequest(req, testSecret, "auth-gateway", "crm-service", time.Now(), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong source, got %d", rec.Code)
	}
}

func TestRequireServiceAuth_ExpiredToken(t *testing.T) {
	handler := RequireServiceAuth(testSecret, "crm-service", AllowedSources("ai-service"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	req := httptest.NewRequest("GET", "/internal/ai/customers/1", nil)
	if err := SignRequest(req, testSecret, "ai-service", "crm-service", time.Now().Add(-10*time.Minute), time.Minute); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d", rec.Code)
	}
}

func TestSignRequest_NoFallbackForBadSecret(t *testing.T) {
	req := httptest.NewRequest("GET", "/internal/ai/users", nil)
	// Short secret must fail — never issue a weak token.
	if err := SignRequest(req, "short", "ai-service", "iam-service", time.Now(), time.Minute); err == nil {
		t.Fatal("expected error for short secret")
	}
}
