package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/config"
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
	}}

	tests := map[string]string{
		"/api/admin/users":          "http://iam",
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
