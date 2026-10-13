package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsRisk(t *testing.T) {
	path := writePolicy(t, `
routes:
  - id: public
    path: /public/**
    auth: false
  - id: private
    path: /private/**
    auth: true
`)

	pol, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := pol.Routes[0].Risk; got != "public" {
		t.Fatalf("public risk = %q", got)
	}
	if got := pol.Routes[1].Risk; got != "medium" {
		t.Fatalf("private risk = %q", got)
	}
}

func TestDocumentRenumberPoliciesOverrideBroadFinanceWrite(t *testing.T) {
	pol, err := Load(filepath.Join("..", "..", "configs", "policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path, permission string
	}{
		{"/api/finance/document-numbers/renumber-requests", "doc.renumber"},
		{"/api/finance/document-numbers/renumber-requests/approve", "doc.renumber"},
		{"/api/finance/document-numbers/renumber-requests/approve-closed", "doc.renumber.locked"},
	}
	for _, tc := range cases {
		matched, err := pol.Match(tc.path, "POST")
		if err != nil {
			t.Fatalf("Match(%s): %v", tc.path, err)
		}
		if len(matched.Route.Permissions) != 1 || matched.Route.Permissions[0] != tc.permission {
			t.Errorf("Match(%s) permissions=%v, want [%s]", tc.path, matched.Route.Permissions, tc.permission)
		}
	}
}

func TestScheduledJobPoliciesOverrideBroadPlatformRoutes(t *testing.T) {
	pol, err := Load(filepath.Join("..", "..", "configs", "policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path, id, permission string
	}{
		{"GET", "/api/platform/scheduled-jobs", "platform-jobs-read", "platform.read"},
		{"GET", "/api/platform/scheduled-jobs/ai-eval", "platform-jobs-read", "platform.read"},
		{"GET", "/api/platform/scheduled-jobs/ai-eval/runs", "platform-jobs-read", "platform.read"},
		{"GET", "/api/platform/scheduled-jobs/runs/0192f0c4-7b7a-7cc4-9a51-6f0a4c2b1d11", "platform-jobs-read", "platform.read"},
		{"PATCH", "/api/platform/scheduled-jobs/ai-eval", "platform-jobs-manage", "platform.manage"},
		{"POST", "/api/platform/scheduled-jobs/ai-eval/run", "platform-jobs-manage", "platform.manage"},
		{"POST", "/api/platform/scheduled-jobs/cron-preview", "platform-jobs-manage", "platform.manage"},
	}
	for _, tc := range cases {
		matched, err := pol.Match(tc.path, tc.method)
		if err != nil {
			t.Fatalf("Match(%s %s): %v", tc.method, tc.path, err)
		}
		if matched.Route.ID != tc.id || !matched.RequireAuth {
			t.Errorf("Match(%s %s) = %s (auth=%v), want %s", tc.method, tc.path, matched.Route.ID, matched.RequireAuth, tc.id)
		}
		if len(matched.Route.Permissions) != 1 || matched.Route.Permissions[0] != tc.permission {
			t.Errorf("Match(%s %s) permissions=%v, want [%s]", tc.method, tc.path, matched.Route.Permissions, tc.permission)
		}
	}
	// The broad platform routes still own everything else.
	if m, err := pol.Match("/api/platform/jobs", "GET"); err != nil || m.Route.ID != "platform-read" {
		t.Errorf("GET /api/platform/jobs must stay on platform-read, got %+v, %v", m, err)
	}
}

func TestCalendarStatusIsReadableByAnySignedInUser(t *testing.T) {
	pol, err := Load(filepath.Join("..", "..", "configs", "policy.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := pol.Match("/api/platform/calendar/status", "GET")
	if err != nil {
		t.Fatal(err)
	}
	if m.Route.ID != "platform-calendar-status" || !m.RequireAuth || len(m.Route.Permissions) != 0 {
		t.Errorf("GET calendar/status = %s (auth=%v, perms=%v), want platform-calendar-status, auth, no permissions",
			m.Route.ID, m.RequireAuth, m.Route.Permissions)
	}
	// Running EOD and the rest of the calendar stay on the broad platform routes.
	if m, err := pol.Match("/api/platform/calendar/eod", "POST"); err != nil || m.Route.ID != "platform-write" {
		t.Errorf("POST calendar/eod must stay on platform-write, got %+v, %v", m, err)
	}
	if m, err := pol.Match("/api/platform/calendar/holidays", "GET"); err != nil || m.Route.ID != "platform-read" {
		t.Errorf("GET calendar/holidays must stay on platform-read, got %+v, %v", m, err)
	}
}

func TestLoadRejectsInvalidRisk(t *testing.T) {
	path := writePolicy(t, `
routes:
  - id: weird
    path: /weird/**
    auth: true
    risk: extreme
`)

	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid risk error")
	}
}

func TestLoadRejectsInvalidRouteDefinitions(t *testing.T) {
	tests := map[string]string{
		"missing id": `
routes:
  - path: /private
    auth: true
`,
		"missing leading slash": `
routes:
  - id: private
    path: private
    auth: true
`,
		"public permission": `
routes:
  - id: public
    path: /public
    auth: false
    permissions: [public.read]
`,
		"public risk mismatch": `
routes:
  - id: public
    path: /public
    auth: false
    risk: low
`,
		"protected public risk": `
routes:
  - id: private
    path: /private
    auth: true
    risk: public
`,
		"invalid method": `
routes:
  - id: private
    path: /private
    methods: [TRACE]
    auth: true
`,
		"duplicate method": `
routes:
  - id: private
    path: /private
    methods: [GET, get]
    auth: true
`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writePolicy(t, data)); err == nil {
				t.Fatal("expected invalid route definition error")
			}
		})
	}
}

func writePolicy(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
