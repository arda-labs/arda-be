package handler

import (
	"strings"

	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// Provider-URL policy shared by the model-profile endpoints. The legacy
// /api/ai/settings CRUD (tenant-wide single model row) was retired in favour
// of profiles and removed with audit-2026-09 item A5.

// testConnectionResponse is the model-probe envelope returned by the profile
// test endpoint. It lives here to keep the check-json-tags baseline entry
// (settings.go#testConnectionResponse) valid until the camelCase migration
// wave renames the fields.
type testConnectionResponse struct {
	Success   bool   `json:"success"`
	LatencyMs int64  `json:"latencyMs"`
	ModelID   string `json:"modelId,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

func validateProviderURL(rawURL string, allowLocal bool) error {
	return ardahttp.ValidateEgressURL(rawURL, allowLocal)
}

// baseURLAllowed reports whether rawURL matches the gateway allowlist.
// Entries are URL prefixes with path-boundary semantics, so
// "https://gateway.example/v1/acct/gw" permits "/v1/acct/gw/openai" but not
// "/v1/acct/other". An empty allowlist disables enforcement; only
// ValidateEgressURL applies.
func baseURLAllowed(allowlist []string, rawURL string) bool {
	if len(allowlist) == 0 {
		return true
	}
	candidate := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if candidate == "" {
		return false
	}
	for _, entry := range allowlist {
		prefix := strings.TrimRight(strings.TrimSpace(entry), "/")
		if prefix == "" {
			continue
		}
		if candidate == prefix || strings.HasPrefix(candidate, prefix+"/") {
			return true
		}
	}
	return false
}

func isMaskedSecret(value string) bool {
	return strings.Contains(value, "...")
}

func maskAPIKey(key string) string {
	clean := strings.TrimSpace(key)
	if len(clean) <= 8 {
		if clean == "" {
			return ""
		}
		return "••••••••"
	}
	return clean[:4] + "..." + clean[len(clean)-4:]
}
