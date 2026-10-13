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

const loadSnapshotYAML = `
`

// loadSnapshotEnv lists every environment variable Load() reads, with a value
// that differs from both the Go default and the YAML value.
var loadSnapshotEnv = map[string]string{
	"AI_AGENT_MAX_STEPS": "99",
	"AI_AGENT_RUN_TIMEOUT_SECONDS": "99",
	"AI_ENABLE_HITL_PROPOSALS": "false",
	"AI_ENABLE_READ_TOOLS": "false",
	"AI_EVAL_SET": "env-AI_EVAL_SET",
	"AI_EVAL_TENANT": "env-AI_EVAL_TENANT",
	"AI_MODE": "env-AI_MODE",
	"AI_MODEL_BASE_URL_ALLOWLIST": " env/a , env/b ,",
	"AI_MODEL_GATEWAY_TOKEN": "env-AI_MODEL_GATEWAY_TOKEN",
	"AI_MODEL_SYSTEM_PROMPT": "env-AI_MODEL_SYSTEM_PROMPT",
	"AI_NATS_URL": "env-AI_NATS_URL",
	"AI_RAG_EMBEDDING_API_KEY": "env-AI_RAG_EMBEDDING_API_KEY",
	"AI_RAG_EMBEDDING_BASE_URL": "env-AI_RAG_EMBEDDING_BASE_URL",
	"AI_RAG_EMBEDDING_CACHE_TTL_SECONDS": "0",
	"AI_RAG_EMBEDDING_DIMENSIONS": "99",
	"AI_RAG_EMBEDDING_MODEL": "env-AI_RAG_EMBEDDING_MODEL",
	"AI_RAG_MIN_SIMILARITY": "0.25",
	"AI_RAG_QUERY_REWRITE": "false",
	"AI_RAG_REQUIRE_EMBEDDING": "false",
	"AI_RAG_RERANKER_API_KEY": "env-AI_RAG_RERANKER_API_KEY",
	"AI_RAG_RERANKER_BASE_URL": "env-AI_RAG_RERANKER_BASE_URL",
	"AI_RAG_RERANKER_MODEL": "env-AI_RAG_RERANKER_MODEL",
	"AI_RATE_LIMIT_PER_MINUTE": "99",
	"APP_NAME": "env-APP_NAME",
	"ARDA_SERVICE_AUTH_SECRET": "env-ARDA_SERVICE_AUTH_SECRET",
	"CAPITAL_SERVICE_URL": "http://env-capital-service-url/",
	"CRM_SERVICE_URL": "http://env-crm-service-url/",
	"DATABASE_DSN": "env-DATABASE_DSN",
	"DB_CONN_MAX_IDLE_SECONDS": "99",
	"DB_MAX_IDLE_CONNS": "99",
	"DB_MAX_OPEN_CONNS": "99",
	"DEPOSIT_SERVICE_URL": "http://env-deposit-service-url/",
	"FINANCE_SERVICE_URL": "http://env-finance-service-url/",
	"HRM_SERVICE_URL": "http://env-hrm-service-url/",
	"HTTP_ADDR": "env-HTTP_ADDR",
	"IAM_SERVICE_URL": "http://env-iam-service-url/",
	"LOAN_SERVICE_URL": "http://env-loan-service-url/",
	"MDM_SERVICE_URL": "http://env-mdm-service-url/",
	"MEDIA_SERVICE_URL": "http://env-media-service-url/",
	"NATS_URL": "env-NATS_URL",
	"NOTIFICATION_SERVICE_URL": "http://env-notification-service-url/",
	"PLATFORM_SERVICE_URL": "http://env-platform-service-url/",
	"PROBLEM_DOCS_URL": "env-PROBLEM_DOCS_URL",
	"REDIS_URL": "env-REDIS_URL",
	"STATISTICAL_SERVICE_URL": "http://env-statistical-service-url/",
	"WORKFLOW_SERVICE_URL": "http://env-workflow-service-url/",
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
