package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/policy"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

func TestStripAuthContextHeaders(t *testing.T) {
	header := http.Header{
		"X-User-Id":        {"user-1"},
		"X-Actor-User-Id":  {"actor-1"},
		"X-Target-User-Id": {"target-1"},
		"X-Auth-Checked":   {"true"},
		"X-Auth-Time":      {"123"},
		"X-Org-Id":         {"org-forged"},
		"X-User-Org-Ids":   {"org-forged-1,org-forged-2"},
		"Authorization":    {"Bearer token"},
	}

	stripAuthContextHeaders(header)

	for _, key := range []string{"X-User-Id", "X-Actor-User-Id", "X-Target-User-Id", "X-Auth-Checked", "X-Auth-Time", "X-Org-Id", "X-User-Org-Ids"} {
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
		AIServiceURL:       "http://ai",
		RAGServiceURL:      "http://rag",
	}}

	tests := map[string]string{
		"/api/admin/users":          "http://iam",
		"/api/iam/me":               "http://iam",
		"/api/unknown":              "",
		"/api/platform/parameters":  "http://platform",
		"/api/finance/accounts":     "http://finance",
		"/api/media/files":          "http://media",
		"/api/workflow/processes":   "http://workflow",
		"/api/crm/customers":        "http://crm",
		"/api/notifications/unread": "http://notification",
		"/api/mdm/items":            "http://mdm",
		"/api/ai/agent":             "http://ai",
		"/api/rag/query":            "http://rag",
		"/api/rag/sources":          "http://rag",
	}
	for path, want := range tests {
		if got := handler.upstreamBaseURL(path); got != want {
			t.Fatalf("%s routed to %q, want %q", path, got, want)
		}
	}
}

func TestProxyRequiresAuthWhenPolicyDoesNotMatch(t *testing.T) {
	called := false
	handler := &BFFHandler{
		cfg:    config.Config{IAMServiceURL: "http://iam"},
		policy: &policy.Policy{},
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		})},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	req.Header.Set("X-Request-Id", "req-unknown")
	rec := httptest.NewRecorder()

	handler.Proxy(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if called {
		t.Fatal("unknown route was sent upstream")
	}
	if got := rec.Header().Get("X-Request-Id"); got != "req-unknown" {
		t.Fatalf("request id = %q, want req-unknown", got)
	}
}

func TestProxyRejectsUnverifiedActiveOrganization(t *testing.T) {
	called := false
	store := session.NewMemoryStore()
	handler := &BFFHandler{
		cfg:    config.Config{IAMServiceURL: "http://iam", SessionCookieName: "arda_sid"},
		store:  store,
		policy: &policy.Policy{Routes: []policy.Route{{ID: "known", Path: "/api/iam/known", Auth: true}}},
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		})},
	}
	sess := &session.Session{User: &session.UserInfo{
		UserID: "u1", Subject: "s1", AuthVersion: 1, GroupIDs: []string{}, TenantMemberships: []session.TenantMembership{}, GlobalCapabilitiesLoaded: true, OrgIDs: []string{"org-1"},
	}}
	if err := store.Create(nil, sess, time.Minute); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/iam/known", nil)
	req.Header.Set("X-Org-Id", "org-2")
	req.AddCookie(&http.Cookie{Name: "arda_sid", Value: sess.ID})
	rec := httptest.NewRecorder()

	handler.Proxy(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Fatal("request with an unverified organization was sent upstream")
	}
}

func TestProxyOwnsCorrelationHeadersAtPublicBoundary(t *testing.T) {
	store := session.NewMemoryStore()
	handler := &BFFHandler{
		cfg:    config.Config{IAMServiceURL: "http://iam", SessionCookieName: "arda_sid"},
		store:  store,
		policy: &policy.Policy{Routes: []policy.Route{{ID: "known", Path: "/api/iam/known", Auth: true}}},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestID := req.Header.Get("X-Request-Id")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"X-Request-Id": {"upstream-id"}, "X-Trace-Id": {"upstream-trace"}},
				Body:       io.NopCloser(strings.NewReader(`{"request_id":"` + requestID + `"}`)),
				Request:    req,
			}, nil
		})},
	}
	sess := &session.Session{AccessToken: "access-token", User: &session.UserInfo{
		UserID: "u1", Subject: "s1", TenantID: "tenant-a", AuthVersion: 1, GroupIDs: []string{}, TenantMemberships: []session.TenantMembership{}, GlobalCapabilitiesLoaded: true,
	}}
	if err := store.Create(nil, sess, time.Minute); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/iam/known", nil)
	req.Header.Set("X-Request-Id", "browser-request")
	req.Header.Set("X-Trace-Id", "browser-trace")
	req.AddCookie(&http.Cookie{Name: "arda_sid", Value: sess.ID})
	rec := httptest.NewRecorder()

	handler.Proxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "browser-request" {
		t.Fatalf("request id = %q, want browser-request", got)
	}
	if got := rec.Header().Get("X-Trace-Id"); got != "browser-trace" {
		t.Fatalf("trace id = %q, want browser-trace", got)
	}
	if !strings.Contains(rec.Body.String(), `"request_id":"browser-request"`) {
		t.Fatalf("body did not preserve forwarded correlation: %s", rec.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
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
	if sessionUserComplete(&session.UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 2, GroupIDs: []string{}, TenantMemberships: []session.TenantMembership{}, GlobalCapabilitiesLoaded: true}) != true {
		t.Fatal("expected user with id, subject, auth version, group ids, and tenant context to be complete")
	}
	for name, user := range map[string]*session.UserInfo{
		"nil":                    nil,
		"missing id":             {Subject: "s1", AuthVersion: 2, GroupIDs: []string{}},
		"missing sub":            {UserID: "u1", AuthVersion: 2, GroupIDs: []string{}},
		"zero version":           {UserID: "u1", Subject: "s1", GroupIDs: []string{}},
		"missing group ids":      {UserID: "u1", Subject: "s1", AuthVersion: 2, TenantMemberships: []session.TenantMembership{}, GlobalCapabilitiesLoaded: true},
		"missing tenant context": {UserID: "u1", Subject: "s1", AuthVersion: 2, GroupIDs: []string{}},
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

func TestResolveSessionUserFailsClosedWithoutIAMClient(t *testing.T) {
	handler := &BFFHandler{}
	user, ok := handler.resolveSessionUser(
		context.Background(),
		&session.UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 1, GroupIDs: []string{}},
		false,
	)
	if ok || user != nil {
		t.Fatalf("missing IAM client resolved a session user: user=%#v ok=%v", user, ok)
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
		User: &session.UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 1, GroupIDs: []string{}, TenantMemberships: []session.TenantMembership{}, GlobalCapabilitiesLoaded: true},
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

func TestMeReadsTheBFFSessionCookie(t *testing.T) {
	store := session.NewMemoryStore()
	handler := &BFFHandler{cfg: config.Config{SessionCookieName: "arda_sid"}, store: store}
	sess := &session.Session{
		User: &session.UserInfo{
			UserID:                   "u1",
			Subject:                  "s1",
			AuthVersion:              1,
			GroupIDs:                 []string{},
			TenantMemberships:        []session.TenantMembership{},
			GlobalCapabilitiesLoaded: true,
		},
	}
	if err := store.Create(nil, sess, time.Minute); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "arda_sid", Value: sess.ID})
	rec := httptest.NewRecorder()

	handler.Me(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"userId":"u1"`) {
		t.Fatalf("response did not contain authenticated user: %s", rec.Body.String())
	}
	var envelope struct {
		Result   *session.UserInfo `json:"result"`
		Success  bool              `json:"success"`
		Errors   []any             `json:"errors"`
		Messages []string          `json:"messages"`
		Meta     struct {
			RequestID string `json:"request_id"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode canonical response: %v", err)
	}
	if !envelope.Success || envelope.Result == nil || envelope.Result.UserID != "u1" {
		t.Fatalf("unexpected canonical response: %#v", envelope)
	}
	if len(envelope.Errors) != 0 || envelope.Messages == nil || envelope.Meta.RequestID == "" {
		t.Fatalf("canonical metadata missing: %#v", envelope)
	}
	if got := rec.Header().Get("X-Request-Id"); got != envelope.Meta.RequestID {
		t.Fatalf("request id header = %q, meta = %q", got, envelope.Meta.RequestID)
	}
}

func TestAcceptConsentFailsClosedWhenHydraConsentLookupFails(t *testing.T) {
	calls := 0
	handler := &BFFHandler{
		cfg: config.Config{HydraAdminURL: "http://hydra"},
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: req}, nil
		})},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/accept-consent", strings.NewReader(`{"consent_challenge":"challenge-1"}`))
	rec := httptest.NewRecorder()

	handler.AcceptConsent(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if calls != 1 {
		t.Fatalf("hydra calls = %d, want lookup only", calls)
	}
}

func TestOAuthErrorCallbackConsumesAndValidatesStateBeforeRedirect(t *testing.T) {
	store := session.NewMemoryStore()
	handler := &BFFHandler{
		cfg:   config.Config{FrontendOrigin: "https://arda.io.vn"},
		store: store,
	}
	stateValue, _ := json.Marshal(oauthStateCookie{State: "state-1", CodeVerifier: "verifier-1", ReturnTo: "/"})
	if err := store.SetOAuthState(nil, "state-1", string(stateValue), time.Minute); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?state=state-1&error=access_denied", nil)
	rec := httptest.NewRecorder()

	handler.OAuthCallback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "https://arda.io.vn/login?error=access_denied" {
		t.Fatalf("location = %q", got)
	}
	consumed, err := store.ConsumeOAuthState(nil, "state-1")
	if err != nil {
		t.Fatal(err)
	}
	if consumed != "" {
		t.Fatalf("oauth state remained replayable: %q", consumed)
	}
}
