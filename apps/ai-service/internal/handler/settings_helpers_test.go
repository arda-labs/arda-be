package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

// fakeSettingsStore implements repository.TenantSettingsStore for the agent
// model-resolution tests. The legacy upsert path was removed with the retired
// /api/ai/settings CRUD (audit-2026-09 A5): profiles are the only writer.
type fakeSettingsStore struct {
	fakeToolRunStore
	settings map[string]*repository.TenantSettings
}

func (s *fakeSettingsStore) GetTenantSettings(_ context.Context, tenantID string) (*repository.TenantSettings, error) {
	if item, ok := s.settings[tenantID]; ok {
		return item, nil
	}
	return nil, repository.ErrTenantSettingsNotFound
}

func adminGatewayHeaders(req *http.Request) {
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", "admin-1")
	req.Header.Set("X-Tenant-Id", "tenant-test")
	req.Header.Set("X-Permissions", "ai.assistant.use,ai.admin")
}

func TestBaseURLAllowed(t *testing.T) {
	gateway := "https://gateway.example.com/v1/acc/gw"
	cases := []struct {
		name      string
		allowlist []string
		url       string
		want      bool
	}{
		{"disabled when empty", nil, "https://anything.example.com/v1", true},
		{"exact match", []string{gateway}, gateway, true},
		{"subpath match", []string{gateway}, gateway + "/openai", true},
		{"trailing slash normalized", []string{gateway + "/"}, gateway + "/openai", true},
		{"path boundary respected", []string{gateway}, "https://gateway.example.com/v1/acc/gwother", false},
		{"different host", []string{gateway}, "https://evil.example.com/v1/acc/gw", false},
		{"metadata blocked by prefix mismatch", []string{gateway}, "http://169.254.169.254/latest", false},
		{"blank candidate", []string{gateway}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := baseURLAllowed(tc.allowlist, tc.url); got != tc.want {
				t.Fatalf("baseURLAllowed(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestSettings_ProviderURLBlocksLocalByDefault(t *testing.T) {
	if err := validateProviderURL("http://127.0.0.1:11434/v1", false); err == nil {
		t.Fatal("provider URL validation must block loopback addresses by default")
	}
	if err := validateProviderURL("http://127.0.0.1:11434/v1", true); err != nil {
		t.Fatalf("local development override should allow loopback: %v", err)
	}
}
