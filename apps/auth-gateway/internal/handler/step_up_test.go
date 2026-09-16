package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

// Step-up must verify the OTP through the service-authenticated internal IAM
// route: the public /api/iam/me/mfa/verify route is actor-scoped and rejects
// server-to-server callers without a verified X-User-Id (regression: step-up
// reported "invalid code" for every correct TOTP after the route hardening).
func TestStepUpVerifiesMFAThroughInternalServiceRoute(t *testing.T) {
	const userID = "00000000-0000-0000-0000-000000000001"

	var verifyCalls int
	var serviceAuth string
	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/iam/me/mfa/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"is_enrolled":true}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/internal/iam/users/"+userID+"/mfa/verify":
			verifyCalls++
			serviceAuth = r.Header.Get("X-Service-Auth")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"verified"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer iam.Close()

	store := session.NewMemoryStore()
	sess := &session.Session{
		User:     &session.UserInfo{UserID: userID},
		AuthTime: time.Now().Add(-time.Hour),
	}
	if err := store.Create(context.Background(), sess, time.Hour); err != nil {
		t.Fatalf("create session: %v", err)
	}

	h := &BFFHandler{
		iamClient: iamclient.New(iam.URL, "0123456789abcdef0123456789abcdef"),
		store:     store,
		cfg: config.Config{
			SessionCookieName: session.DefaultCookieName,
			SessionTTL:        3600,
			RecentAuthWindow:  300,
		},
		logger:           slog.New(slog.DiscardHandler),
		cache:            newUserContextCache(time.Minute),
		resolveInflight:  map[string]*sessionUserResolveCall{},
		httpClient:       &http.Client{Timeout: 5 * time.Second},
		streamHTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/step-up", strings.NewReader(`{"code":"482913"}`))
	req.AddCookie(&http.Cookie{Name: session.DefaultCookieName, Value: sess.ID})
	rec := httptest.NewRecorder()
	h.StepUp(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("step-up status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if verifyCalls != 1 {
		t.Fatalf("internal verify calls = %d, want 1", verifyCalls)
	}
	if serviceAuth == "" {
		t.Fatal("internal verify must carry the service-auth header")
	}
}
