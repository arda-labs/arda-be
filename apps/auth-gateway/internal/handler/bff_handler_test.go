package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
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
	if sessionUserComplete(&session.UserInfo{UserID: "u1", Subject: "s1", AuthVersion: 2}) != true {
		t.Fatal("expected user with id, subject, and auth version to be complete")
	}
	for name, user := range map[string]*session.UserInfo{
		"nil":          nil,
		"missing id":   {Subject: "s1", AuthVersion: 2},
		"missing sub":  {UserID: "u1", AuthVersion: 2},
		"zero version": {UserID: "u1", Subject: "s1"},
	} {
		if sessionUserComplete(user) {
			t.Fatalf("%s user should be incomplete", name)
		}
	}
}
