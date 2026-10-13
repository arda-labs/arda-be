package config

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// Regression snapshot of Load(): the same YAML file and environment must keep
// producing the same Config across loader refactors. Regenerate only when the
// configuration contract intentionally changes:
//
//	go test ./internal/config -run TestLoadSnapshot -update
var updateLoadSnapshot = flag.Bool("update", false, "rewrite testdata/load_snapshot.json")

const loadSnapshotYAML = `app_name: "yaml-app_name"
http_addr: "yaml-http_addr"
log_level: "yaml-log_level"
token_strategy: "yaml-token_strategy"
jwt_secret: "yaml-jwt_secret"
jwt_issuer: "yaml-jwt_issuer"
jwt_audience: "yaml-jwt_audience"
jwks_url: "yaml-jwks_url"
introspection_url: "yaml-introspection_url"
introspection_client_id: "yaml-introspection_client_id"
introspection_client_secret: "yaml-introspection_client_secret"
iam_service_url: "yaml-iam_service_url"
iam_context_cache_ttl_seconds: 1012
slow_request_log_enabled: true
slow_request_log_threshold_ms: 1014
cors_allowed_origins: "yaml-cors_allowed_origins"
frontend_origin: "yaml-frontend_origin"
platform_service_url: "yaml-platform_service_url"
finance_service_url: "yaml-finance_service_url"
media_service_url: "yaml-media_service_url"
workflow_service_url: "yaml-workflow_service_url"
crm_service_url: "yaml-crm_service_url"
hrm_service_url: "yaml-hrm_service_url"
notification_service_url: "yaml-notification_service_url"
mdm_service_url: "yaml-mdm_service_url"
ai_service_url: "yaml-ai_service_url"
rag_service_url: "yaml-rag_service_url"
loan_service_url: "yaml-loan_service_url"
deposit_service_url: "yaml-deposit_service_url"
capital_service_url: "yaml-capital_service_url"
statistical_service_url: "yaml-statistical_service_url"
policy_file: "yaml-policy_file"
redis_url: "yaml-redis_url"
session_store: "yaml-session_store"
session_cookie_name: "yaml-session_cookie_name"
session_cookie_domain: "yaml-session_cookie_domain"
session_ttl_seconds: 1036
session_auth_check_interval_seconds: 1037
recent_auth_window_seconds: 1038
cookie_secure: true
cookie_same_site: "yaml-cookie_same_site"
kratos_public_url: "yaml-kratos_public_url"
hydra_admin_url: "yaml-hydra_admin_url"
hydra_public_url: "yaml-hydra_public_url"
oauth_client_id: "yaml-oauth_client_id"
oauth_redirect_uri: "yaml-oauth_redirect_uri"
oauth_redirect_uris: "yaml-oauth_redirect_uris"
`

// loadSnapshotEnv lists every environment variable Load() reads, with a value
// that differs from both the Go default and the YAML value.
var loadSnapshotEnv = map[string]string{
	"AI_SERVICE_URL": "env-AI_SERVICE_URL",
	"APP_NAME": "env-APP_NAME",
	"ARDA_SERVICE_AUTH_SECRET": "env-ARDA_SERVICE_AUTH_SECRET",
	"CAPITAL_SERVICE_URL": "env-CAPITAL_SERVICE_URL",
	"COOKIE_SAMESITE": "env-COOKIE_SAMESITE",
	"COOKIE_SECURE": "false",
	"CORS_ALLOWED_ORIGINS": "env-CORS_ALLOWED_ORIGINS",
	"CRM_SERVICE_URL": "env-CRM_SERVICE_URL",
	"DEPOSIT_SERVICE_URL": "env-DEPOSIT_SERVICE_URL",
	"FINANCE_SERVICE_URL": "env-FINANCE_SERVICE_URL",
	"FRONTEND_ORIGIN": "env-FRONTEND_ORIGIN",
	"HRM_SERVICE_URL": "env-HRM_SERVICE_URL",
	"HTTP_ADDR": "env-HTTP_ADDR",
	"HYDRA_ADMIN_URL": "env-HYDRA_ADMIN_URL",
	"HYDRA_PUBLIC_URL": "env-HYDRA_PUBLIC_URL",
	"IAM_CONTEXT_CACHE_TTL_SECONDS": "99",
	"IAM_SERVICE_URL": "env-IAM_SERVICE_URL",
	"INTROSPECTION_CLIENT_ID": "env-INTROSPECTION_CLIENT_ID",
	"INTROSPECTION_CLIENT_SECRET": "env-INTROSPECTION_CLIENT_SECRET",
	"INTROSPECTION_URL": "env-INTROSPECTION_URL",
	"JWKS_URL": "env-JWKS_URL",
	"JWT_AUDIENCE": "env-JWT_AUDIENCE",
	"JWT_ISSUER": "env-JWT_ISSUER",
	"JWT_SECRET": "env-JWT_SECRET",
	"KRATOS_PUBLIC_URL": "env-KRATOS_PUBLIC_URL",
	"LOAN_SERVICE_URL": "env-LOAN_SERVICE_URL",
	"LOG_LEVEL": "env-LOG_LEVEL",
	"MDM_SERVICE_URL": "env-MDM_SERVICE_URL",
	"MEDIA_SERVICE_URL": "env-MEDIA_SERVICE_URL",
	"NOTIFICATION_SERVICE_URL": "env-NOTIFICATION_SERVICE_URL",
	"OAUTH_CLIENT_ID": "env-OAUTH_CLIENT_ID",
	"OAUTH_REDIRECT_URI": "env-OAUTH_REDIRECT_URI",
	"OAUTH_REDIRECT_URIS": "env-OAUTH_REDIRECT_URIS",
	"PLATFORM_SERVICE_URL": "env-PLATFORM_SERVICE_URL",
	"POLICY_FILE": "env-POLICY_FILE",
	"RAG_SERVICE_URL": "env-RAG_SERVICE_URL",
	"RECENT_AUTH_WINDOW_SECONDS": "99",
	"REDIS_URL": "env-REDIS_URL",
	"SESSION_AUTH_CHECK_INTERVAL_SECONDS": "99",
	"SESSION_COOKIE_DOMAIN": "env-SESSION_COOKIE_DOMAIN",
	"SESSION_COOKIE_NAME": "env-SESSION_COOKIE_NAME",
	"SESSION_STORE": "env-SESSION_STORE",
	"SESSION_TTL_SECONDS": "99",
	"SLOW_REQUEST_LOG_ENABLED": "false",
	"SLOW_REQUEST_LOG_THRESHOLD_MS": "99",
	"STATISTICAL_SERVICE_URL": "env-STATISTICAL_SERVICE_URL",
	"TOKEN_STRATEGY": "env-TOKEN_STRATEGY",
	"WORKFLOW_SERVICE_URL": "env-WORKFLOW_SERVICE_URL",
}

func TestLoadSnapshot(t *testing.T) {
	cases := []struct {
		name     string
		withYAML bool
		env      string // "unset", "set" or "empty"
	}{
		{name: "defaults", withYAML: false, env: "unset"},
		{name: "yaml_only", withYAML: true, env: "unset"},
		{name: "env_only", withYAML: false, env: "set"},
		{name: "env_over_yaml", withYAML: true, env: "set"},
		{name: "empty_env_keeps_yaml", withYAML: true, env: "empty"},
	}
	goldenPath := filepath.Join("testdata", "load_snapshot.json")
	golden := map[string]json.RawMessage{}
	if data, err := os.ReadFile(goldenPath); err == nil {
		if err := json.Unmarshal(data, &golden); err != nil {
			t.Fatalf("parse %s: %v", goldenPath, err)
		}
	} else if !*updateLoadSnapshot {
		t.Fatalf("read %s: %v (run with -update to create it)", goldenPath, err)
	}
	got := map[string]json.RawMessage{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "missing.yaml")
			if tc.withYAML {
				configPath = filepath.Join(dir, "config.yaml")
				if err := os.WriteFile(configPath, []byte(loadSnapshotYAML), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CONFIG_FILE", configPath)
			for name, value := range loadSnapshotEnv {
				switch tc.env {
				case "set":
					t.Setenv(name, value)
				default:
					t.Setenv(name, "")
				}
			}
			cfg := Load()
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got[tc.name] = data
			if *updateLoadSnapshot {
				return
			}
			want, ok := golden[tc.name]
			if !ok {
				t.Fatalf("golden has no case %q (run with -update)", tc.name)
			}
			var wantBuf bytes.Buffer
			if err := json.Indent(&wantBuf, want, "", "  "); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(wantBuf.Bytes(), data) {
				t.Fatalf("Load() drifted from %s case %q:\n--- want\n%s\n--- got\n%s", goldenPath, tc.name, wantBuf.String(), data)
			}
		})
	}
	if *updateLoadSnapshot {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
