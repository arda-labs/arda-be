package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/policy"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

func TestRecentAuthOKRequiresFreshAuthForHighRisk(t *testing.T) {
	handler := &BFFHandler{
		cfg: config.Config{RecentAuthWindow: 300},
		policy: &policy.Policy{Routes: []policy.Route{{
			ID:      "write",
			Path:    "/api/platform/**",
			Methods: []string{"POST"},
			Auth:    true,
			Risk:    "high",
		}}},
	}
	req := httptest.NewRequest("POST", "/api/platform/things", nil)
	sess := &session.Session{AuthTime: time.Now().Add(-10 * time.Minute)}

	if handler.recentAuthOK(req, sess) {
		t.Fatal("expected stale high-risk auth to fail")
	}
}

func TestRecentAuthOKAllowsLowRiskStaleAuth(t *testing.T) {
	handler := &BFFHandler{
		cfg: config.Config{RecentAuthWindow: 300},
		policy: &policy.Policy{Routes: []policy.Route{{
			ID:      "read",
			Path:    "/api/platform/**",
			Methods: []string{"GET"},
			Auth:    true,
			Risk:    "low",
		}}},
	}
	req := httptest.NewRequest("GET", "/api/platform/things", nil)
	sess := &session.Session{AuthTime: time.Now().Add(-10 * time.Minute)}

	if !handler.recentAuthOK(req, sess) {
		t.Fatal("expected low-risk auth to pass")
	}
}
