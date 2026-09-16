package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHasGlobalAdminCapability(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    bool
	}{
		{"no headers", nil, false},
		{"global admin flag", map[string]string{"X-Global-Admin": "true"}, true},
		{"global admin flag mixed case", map[string]string{"X-Global-Admin": " TRUE "}, true},
		{"global admin false", map[string]string{"X-Global-Admin": "false"}, false},
		{"global role", map[string]string{"X-Global-Roles": "SUPER_ADMIN"}, true},
		{"global roles csv", map[string]string{"X-Global-Roles": "AUDITOR, superadmin"}, true},
		{"global permission", map[string]string{"X-Global-Permissions": "superadmin"}, true},
		{"tenant role must not imply global", map[string]string{"X-Roles": "SUPER_ADMIN"}, false},
		{"tenant permission must not imply global", map[string]string{"X-Permissions": "superadmin"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/platform/calendar/eod", nil)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			if got := hasGlobalAdminCapability(req); got != tc.want {
				t.Fatalf("hasGlobalAdminCapability() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRequireGlobalAdmin(t *testing.T) {
	cases := []struct {
		name       string
		headers    map[string]string
		wantStatus int
	}{
		{
			"verified global admin passes",
			map[string]string{"X-Auth-Checked": "true", "X-Global-Admin": "true"},
			http.StatusOK,
		},
		{
			"verified superadmin role passes",
			map[string]string{"X-Auth-Checked": "true", "X-Global-Roles": "SUPER_ADMIN"},
			http.StatusOK,
		},
		{
			"tenant operator with platform.manage is rejected",
			map[string]string{"X-Auth-Checked": "true", "X-Permissions": "platform.manage"},
			http.StatusForbidden,
		},
		{
			"global claim without verified auth context is rejected",
			map[string]string{"X-Global-Admin": "true"},
			http.StatusForbidden,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/platform/calendar/eod", nil)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			rec := httptest.NewRecorder()
			if got := requireGlobalAdmin(rec, req); got != (tc.wantStatus == http.StatusOK) {
				t.Fatalf("requireGlobalAdmin() = %v, want %v", got, tc.wantStatus == http.StatusOK)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}
