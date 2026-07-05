package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

func TestStripAuthContextHeaders(t *testing.T) {
	header := http.Header{
		"X-User-Id":      {"user-1"},
		"X-Auth-Checked": {"true"},
		"X-Auth-Time":    {"123"},
		"Authorization":  {"Bearer token"},
	}

	stripAuthContextHeaders(header)

	for _, key := range []string{"X-User-Id", "X-Auth-Checked", "X-Auth-Time"} {
		if got := header.Get(key); got != "" {
			t.Fatalf("%s was not stripped: %q", key, got)
		}
	}
	if got := header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization was changed: %q", got)
	}
}

func TestUpstreamBaseURLRoutesKnownAPIPrefixes(t *testing.T) {
	handler := &BFFHandler{cfg: config.Config{
		IAMServiceURL:      "http://iam",
		PlatformServiceURL: "http://platform",
		FinanceServiceURL:  "http://finance",
		MediaServiceURL:    "http://media",
		WorkflowServiceURL: "http://workflow",
		CRMServiceURL:      "http://crm",
		NotificationURL:    "http://notification",
		MDMServiceURL:      "http://mdm",
		ProxyBackendURL:    "http://fallback",
	}}

	tests := map[string]string{
		"/api/admin/users":          "http://iam",
		"/api/iam/me":               "http://iam",
		"/api/unknown":              "http://fallback",
		"/api/platform/parameters":  "http://platform",
		"/api/finance/accounts":     "http://finance",
		"/api/media/files":          "http://media",
		"/api/workflow/processes":   "http://workflow",
		"/api/crm/customers":        "http://crm",
		"/api/notifications/unread": "http://notification",
		"/api/mdm/items":            "http://mdm",
	}
	for path, want := range tests {
		if got := handler.upstreamBaseURL(path); got != want {
			t.Fatalf("%s routed to %q, want %q", path, got, want)
		}
	}
}

func TestProxyRequiresAuthWhenPolicyDoesNotMatch(t *testing.T) {
	handler := &BFFHandler{cfg: config.Config{IAMServiceURL: "http://iam"}}
	req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	rec := httptest.NewRecorder()

	handler.Proxy(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestIsEventStreamRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/notifications/stream", nil)
	req.Header.Set("Accept", "text/event-stream")

	if !isEventStreamRequest(req) {
		t.Fatal("event stream request was not detected")
	}
}

func TestIAMLookupIDsOnlyReturnsUniqueUUIDs(t *testing.T) {
	uuid := "00000000-0000-0000-0000-000000000002"
	got := iamLookupIDs(&session.UserInfo{UserID: uuid, Subject: "super-admin"})
	if !reflect.DeepEqual(got, []string{uuid}) {
		t.Fatalf("ids = %#v, want only %s", got, uuid)
	}

	got = iamLookupIDs(&session.UserInfo{UserID: uuid, Subject: uuid})
	if !reflect.DeepEqual(got, []string{uuid}) {
		t.Fatalf("duplicate ids = %#v, want one %s", got, uuid)
	}
}

func TestSessionUserCompleteRequiresStableIdentityAndAuthVersion(t *testing.T) {
	if sessionUserComplete(&session.UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 2, GroupIDs: []string{}}) != true {
		t.Fatal("expected user with id, subject, auth version, and group ids to be complete")
	}
	for name, user := range map[string]*session.UserInfo{
		"nil":              nil,
		"missing id":       {Subject: "s1", AuthVersion: 2, GroupIDs: []string{}},
		"missing sub":      {UserID: "u1", AuthVersion: 2, GroupIDs: []string{}},
		"zero version":     {UserID: "u1", Subject: "s1", GroupIDs: []string{}},
		"missing group ids": {UserID: "u1", Subject: "s1", AuthVersion: 2},
	} {
		if sessionUserComplete(user) {
			t.Fatalf("%s user should be incomplete", name)
		}
	}
}

func TestSessionUserCacheKeysAllowLegacyVersion(t *testing.T) {
	got := sessionUserCacheKeys("u1", "s1", 0)
	want := []string{"u1:legacy", "s1:legacy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy keys = %#v, want %#v", got, want)
	}
}

func TestCacheSessionUserStoresLegacyFallback(t *testing.T) {
	handler := &BFFHandler{cache: newUserContextCache(time.Minute)}
	handler.cacheSessionUser(
		&session.UserInfo{UserID: "u1", Subject: "s1"},
		&iamclient.UserContext{UserID: "u1", Subject: "s1", AuthVersion: 18},
	)

	uc, ok := handler.cache.get("u1:legacy")
	if !ok {
		t.Fatal("legacy user id cache key was not stored")
	}
	if uc.AuthVersion != 18 {
		t.Fatalf("auth version = %d, want 18", uc.AuthVersion)
	}
}

func TestApplyLoginRememberPolicy(t *testing.T) {
	privileged := loginAcceptRequest{Remember: true, RememberFor: loginRememberMaxAge}
	applyLoginRememberPolicy(&privileged, true)
	if privileged.Remember || privileged.RememberFor != 0 {
		t.Fatalf("privileged remember = (%v, %d), want disabled", privileged.Remember, privileged.RememberFor)
	}

	regular := loginAcceptRequest{Remember: true}
	applyLoginRememberPolicy(&regular, false)
	if !regular.Remember || regular.RememberFor != loginRememberMaxAge {
		t.Fatalf("regular remember = (%v, %d), want 30 days", regular.Remember, regular.RememberFor)
	}

	tooLong := loginAcceptRequest{Remember: true, RememberFor: loginRememberMaxAge + 1}
	applyLoginRememberPolicy(&tooLong, false)
	if tooLong.RememberFor != loginRememberMaxAge {
		t.Fatalf("remember_for = %d, want cap %d", tooLong.RememberFor, loginRememberMaxAge)
	}
}

func TestWebCheckRedirectsMissingSessionToOAuthStart(t *testing.T) {
	handler := &BFFHandler{store: session.NewMemoryStore()}
	req := httptest.NewRequest(http.MethodGet, "/auth/web-check", nil)
	req.Header.Set("X-Forwarded-Uri", "/finance?tab=accounts")
	rec := httptest.NewRecorder()

	handler.WebCheck(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/api/auth/start?return_to=%2Ffinance%3Ftab%3Daccounts" {
		t.Fatalf("location = %q", got)
	}
}

func TestWebCheckAllowsValidBFFSession(t *testing.T) {
	store := session.NewMemoryStore()
	handler := &BFFHandler{cfg: config.Config{SessionCookieName: "arda_sid"}, store: store}
	sess := &session.Session{
		User: &session.UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 1},
	}
	if err := store.Create(nil, sess, time.Minute); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/web-check", nil)
	req.AddCookie(&http.Cookie{Name: "arda_sid", Value: sess.ID})
	rec := httptest.NewRecorder()

	handler.WebCheck(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
