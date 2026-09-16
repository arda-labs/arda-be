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

func TestSessionAuthRefreshDue(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		lastCheck  time.Time
		interval   time.Duration
		forceFresh bool
		want       bool
	}{
		{name: "force fresh always revalidates", lastCheck: now, interval: time.Hour, forceFresh: true, want: true},
		{name: "force fresh on new session", interval: time.Minute, forceFresh: true, want: true},
		{name: "zero interval disables scheduled checks", lastCheck: now.Add(-time.Hour), want: false},
		{name: "negative interval disables scheduled checks", lastCheck: now.Add(-time.Hour), interval: -time.Minute, want: false},
		{name: "legacy session without check timestamp", interval: time.Minute, want: true},
		{name: "not yet due", lastCheck: now.Add(-30 * time.Second), interval: time.Minute, want: false},
		{name: "due at interval boundary", lastCheck: now.Add(-time.Minute), interval: time.Minute, want: true},
		{name: "clock skew is not due", lastCheck: now.Add(time.Minute), interval: time.Minute, want: false},
	}
	for _, test := range tests {
		if got := sessionAuthRefreshDue(now, test.lastCheck, test.interval, test.forceFresh); got != test.want {
			t.Fatalf("%s: due = %v, want %v", test.name, got, test.want)
		}
	}
}

// completeSessionUser mirrors the fields sessionUserComplete requires; tests
// bypass IAM unless the session is due for re-validation.
func completeSessionUser(perms ...string) *session.UserInfo {
	return &session.UserInfo{
		UserID:                   "11111111-1111-1111-1111-111111111111",
		Subject:                  "subject-1",
		AuthVersion:              1,
		GroupIDs:                 []string{},
		TenantMemberships:        []session.TenantMembership{},
		GlobalCapabilitiesLoaded: true,
		Permissions:              perms,
	}
}

func newIAMStubHandler(t *testing.T, intervalSeconds int, status int, body string) (*BFFHandler, *session.MemoryStore, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Service-Auth") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	store := session.NewMemoryStore()
	handler := NewBFFHandler(
		config.Config{SessionCookieName: "arda_sid", SessionAuthCheckInterval: intervalSeconds},
		store,
		iamclient.New(server.URL, strings.Repeat("s", 32)),
		nil,
	)
	return handler, store, &calls
}

func TestEnsureSessionUserRefreshesChangedAuthVersion(t *testing.T) {
	handler, store, _ := newIAMStubHandler(t, 60, http.StatusOK, `{
		"userId":"11111111-1111-1111-1111-111111111111",
		"subject":"subject-1",
		"username":"u1",
		"status":"ACTIVE",
		"authVersion":7,
		"groupIds":[],
		"tenantMemberships":[],
		"globalCapabilitiesLoaded":true,
		"roles":["TENANT_ADMIN"],
		"permissions":["iam.user.read"]
	}`)
	sess := &session.Session{
		User:          completeSessionUser(),
		ExpiresAt:     time.Now().Add(time.Hour),
		LastAuthCheck: time.Now().Add(-2 * time.Minute),
	}
	if err := store.Create(context.Background(), sess, time.Hour); err != nil {
		t.Fatal(err)
	}

	if !handler.ensureSessionUser(context.Background(), sess, false) {
		t.Fatal("scheduled re-validation failed")
	}
	if sess.User.AuthVersion != 7 {
		t.Fatalf("auth version = %d, want 7", sess.User.AuthVersion)
	}
	if !containsString(sess.User.Permissions, "iam.user.read") {
		t.Fatalf("permissions = %#v, want refreshed permission", sess.User.Permissions)
	}
	if sess.LastAuthCheck.IsZero() || time.Since(sess.LastAuthCheck) > time.Minute {
		t.Fatalf("last auth check = %v, want now", sess.LastAuthCheck)
	}
}

func TestEnsureSessionUserRevokesSessionWhenIAMNoLongerResolvesUser(t *testing.T) {
	handler, store, _ := newIAMStubHandler(t, 60, http.StatusNotFound, `{"error":"user not found"}`)
	sess := &session.Session{
		User:          completeSessionUser(),
		ExpiresAt:     time.Now().Add(time.Hour),
		LastAuthCheck: time.Now().Add(-2 * time.Minute),
	}
	if err := store.Create(context.Background(), sess, time.Hour); err != nil {
		t.Fatal(err)
	}

	if handler.ensureSessionUser(context.Background(), sess, false) {
		t.Fatal("revoked user was accepted")
	}
	stored, err := store.Get(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored != nil {
		t.Fatalf("session was not deleted after revocation: %#v", stored.ID)
	}
}

func TestEnsureSessionUserRevokesDisabledAccount(t *testing.T) {
	handler, store, _ := newIAMStubHandler(t, 60, http.StatusOK, `{
		"userId":"11111111-1111-1111-1111-111111111111",
		"subject":"subject-1",
		"username":"u1",
		"status":"DISABLED",
		"authVersion":9,
		"groupIds":[],
		"tenantMemberships":[],
		"globalCapabilitiesLoaded":true,
		"roles":["TENANT_ADMIN"],
		"permissions":["iam.user.read"]
	}`)
	sess := &session.Session{
		User:          completeSessionUser("iam.user.read"),
		ExpiresAt:     time.Now().Add(time.Hour),
		LastAuthCheck: time.Now().Add(-2 * time.Minute),
	}
	if err := store.Create(context.Background(), sess, time.Hour); err != nil {
		t.Fatal(err)
	}

	if handler.ensureSessionUser(context.Background(), sess, false) {
		t.Fatal("disabled account kept its session")
	}
	if stored, _ := store.Get(context.Background(), sess.ID); stored != nil {
		t.Fatalf("disabled account session was not deleted: %#v", stored.ID)
	}
}

func TestEnsureSessionUserKeepsSessionWhenIAMIsUnavailable(t *testing.T) {
	handler, store, _ := newIAMStubHandler(t, 60, http.StatusInternalServerError, `{}`)
	lastCheck := time.Now().Add(-2 * time.Minute)
	sess := &session.Session{
		User:          completeSessionUser(),
		ExpiresAt:     time.Now().Add(time.Hour),
		LastAuthCheck: lastCheck,
	}
	if err := store.Create(context.Background(), sess, time.Hour); err != nil {
		t.Fatal(err)
	}

	if !handler.ensureSessionUser(context.Background(), sess, false) {
		t.Fatal("transient IAM failure must not drop a complete session")
	}
	if !sess.LastAuthCheck.Equal(lastCheck) {
		t.Fatalf("last auth check = %v, want retry on the next request", sess.LastAuthCheck)
	}
	stored, _ := store.Get(context.Background(), sess.ID)
	if stored == nil {
		t.Fatal("transient IAM failure deleted the session")
	}

	// The high-risk forceFresh flow keeps failing closed.
	if handler.ensureSessionUser(context.Background(), sess, true) {
		t.Fatal("forceFresh user resolution must fail closed when IAM is unavailable")
	}
	if stored, _ := store.Get(context.Background(), sess.ID); stored == nil {
		t.Fatal("forceFresh failure deleted the session")
	}
}

func TestPolicyRoutesRequiresSessionAndPermission(t *testing.T) {
	store := session.NewMemoryStore()
	pol := &policy.Policy{Routes: []policy.Route{{
		ID: "admin-read", Path: "/api/admin/**", Methods: []string{"GET"}, Auth: true, Permissions: []string{"iam.user.read"},
	}}}
	handler := NewBFFHandler(config.Config{SessionCookieName: "arda_sid"}, store, nil, pol)

	call := func(cookie *session.Session) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/policy-routes", nil)
		if cookie != nil {
			req.AddCookie(&http.Cookie{Name: "arda_sid", Value: cookie.ID})
		}
		rec := httptest.NewRecorder()
		handler.PolicyRoutes(rec, req)
		return rec
	}

	if rec := call(nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	unauthorized := &session.Session{ExpiresAt: time.Now().Add(time.Hour), User: completeSessionUser()}
	if err := store.Create(context.Background(), unauthorized, time.Hour); err != nil {
		t.Fatal(err)
	}
	if rec := call(unauthorized); rec.Code != http.StatusForbidden {
		t.Fatalf("session without iam.user.read status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	reader := &session.Session{ExpiresAt: time.Now().Add(time.Hour), User: completeSessionUser("iam.user.read")}
	if err := store.Create(context.Background(), reader, time.Hour); err != nil {
		t.Fatal(err)
	}
	rec := call(reader)
	if rec.Code != http.StatusOK {
		t.Fatalf("iam.user.read status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "admin-read") {
		t.Fatalf("policy routes not exposed: %s", rec.Body.String())
	}

	admin := &session.Session{ExpiresAt: time.Now().Add(time.Hour), User: completeSessionUser()}
	admin.User.IsGlobalAdmin = true
	if err := store.Create(context.Background(), admin, time.Hour); err != nil {
		t.Fatal(err)
	}
	if rec := call(admin); rec.Code != http.StatusOK {
		t.Fatalf("global admin status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestResolveSessionUserFailsClosedWithoutIAMClient(t *testing.T) {
	handler := &BFFHandler{}
	user, ok, revoked := handler.resolveSessionUser(
		context.Background(),
		&session.UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 1, GroupIDs: []string{}},
		false,
	)
	if ok || revoked || user != nil {
		t.Fatalf("missing IAM client resolved a session user: user=%#v ok=%v revoked=%v", user, ok, revoked)
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

func TestSessionRenewsWhenHalfTheTTLHasPassed(t *testing.T) {
	store := session.NewMemoryStore()
	handler := &BFFHandler{
		cfg:   config.Config{SessionCookieName: "arda_sid", SessionTTL: 3600},
		store: store,
	}

	fresh := &session.Session{ExpiresAt: time.Now().Add(50 * time.Minute), CreatedAt: time.Now(), User: completeSessionUser()}
	stale := &session.Session{ExpiresAt: time.Now().Add(5 * time.Minute), CreatedAt: time.Now().Add(-2 * time.Hour), User: completeSessionUser()}
	capped := &session.Session{ExpiresAt: time.Now().Add(5 * time.Minute), CreatedAt: time.Now().Add(-40 * 24 * time.Hour), User: completeSessionUser()}
	for _, sess := range []*session.Session{fresh, stale, capped} {
		if err := store.Create(nil, sess, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	// Create() stamps CreatedAt; the absolute-cap case needs an older origin.
	capped.CreatedAt = time.Now().Add(-40 * 24 * time.Hour)

	callMe := func(sess *session.Session) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: "arda_sid", Value: sess.ID})
		rec := httptest.NewRecorder()
		handler.Me(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		return rec
	}

	if cookies := callMe(fresh).Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("a fresh session must not re-issue the cookie, got %v", cookies)
	}

	rec := callMe(stale)
	renewed := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "arda_sid" && cookie.MaxAge > 0 {
			renewed = true
		}
	}
	if !renewed {
		t.Fatal("a session past half its TTL must re-issue the cookie")
	}
	stored, err := store.Get(nil, stale.ID)
	if err != nil || stored == nil {
		t.Fatalf("stored session missing: %v", err)
	}
	if time.Until(stored.ExpiresAt) < 30*time.Minute {
		t.Fatalf("stored expiry must slide forward, got %v", time.Until(stored.ExpiresAt))
	}

	if cookies := callMe(capped).Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("the absolute lifetime cap must stop renewal, got %v", cookies)
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
