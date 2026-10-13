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
grpc_addr: "yaml-grpc_addr"
log_level: "yaml-log_level"
database_dsn: "yaml-database_dsn"
nats_url: "yaml-nats_url"
job_notify_default_tenant: "yaml-job_notify_default_tenant"
`

// loadSnapshotEnv lists every environment variable Load() reads, with a value
// that differs from both the Go default and the YAML value.
var loadSnapshotEnv = map[string]string{
	"APP_NAME": "env-APP_NAME",
	"DATABASE_DSN": "env-DATABASE_DSN",
	"GRPC_ADDR": "env-GRPC_ADDR",
	"HTTP_ADDR": "env-HTTP_ADDR",
	"JOB_NOTIFY_DEFAULT_TENANT": "env-JOB_NOTIFY_DEFAULT_TENANT",
	"JOB_TARGET_URLS": "loan-service=http://env-loan:1 , x-service=http://env-x:2,broken,=y",
	"LOG_LEVEL": "env-LOG_LEVEL",
	"NATS_URL": "env-NATS_URL",
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
