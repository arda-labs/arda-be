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
storage_provider: "yaml-storage_provider"
storage_endpoint: "yaml-storage_endpoint"
storage_public_endpoint: "yaml-storage_public_endpoint"
storage_region: "yaml-storage_region"
storage_bucket: "yaml-storage_bucket"
storage_access_key: "yaml-storage_access_key"
storage_secret_key: "yaml-storage_secret_key"
storage_force_path_style: true
upload_max_size_mb: 1014
stream_max_size_mb: 1015
preview_max_size_mb: 1016
gotenberg_url: "yaml-gotenberg_url"
presign_upload_ttl: "25m"
presign_download_ttl: "26m"
require_scan_before_ready: true
temp_file_ttl: "28m"
allowed_upload_mime: ["yaml/allowed_upload_mime/a", "yaml/allowed_upload_mime/b"]
`

// loadSnapshotEnv lists every environment variable Load() reads, with a value
// that differs from both the Go default and the YAML value.
var loadSnapshotEnv = map[string]string{
	"ALLOWED_UPLOAD_MIME": " env/a , env/b ,",
	"APP_NAME": "env-APP_NAME",
	"DATABASE_DSN": "env-DATABASE_DSN",
	"GARAGE_ACCESS_KEY": "env-GARAGE_ACCESS_KEY",
	"GARAGE_BUCKET": "env-GARAGE_BUCKET",
	"GARAGE_ENDPOINT": "env-GARAGE_ENDPOINT",
	"GARAGE_FORCE_PATH_STYLE": "false",
	"GARAGE_PUBLIC_ENDPOINT": "env-GARAGE_PUBLIC_ENDPOINT",
	"GARAGE_REGION": "env-GARAGE_REGION",
	"GARAGE_SECRET_KEY": "env-GARAGE_SECRET_KEY",
	"GOTENBERG_URL": "env-GOTENBERG_URL",
	"GRPC_ADDR": "env-GRPC_ADDR",
	"HTTP_ADDR": "env-HTTP_ADDR",
	"LOG_LEVEL": "env-LOG_LEVEL",
	"MEDIA_ALLOWED_UPLOAD_MIME": " env/a , env/b ,",
	"MEDIA_DATABASE_DSN": "env-MEDIA_DATABASE_DSN",
	"MEDIA_GOTENBERG_URL": "env-MEDIA_GOTENBERG_URL",
	"MEDIA_NATS_URL": "env-MEDIA_NATS_URL",
	"MEDIA_PRESIGN_DOWNLOAD_TTL": "9m",
	"MEDIA_PRESIGN_UPLOAD_TTL": "9m",
	"MEDIA_PREVIEW_MAX_SIZE_MB": "99",
	"MEDIA_REQUIRE_SCAN_BEFORE_READY": "false",
	"MEDIA_STORAGE_ACCESS_KEY": "env-MEDIA_STORAGE_ACCESS_KEY",
	"MEDIA_STORAGE_BUCKET": "env-MEDIA_STORAGE_BUCKET",
	"MEDIA_STORAGE_ENDPOINT": "env-MEDIA_STORAGE_ENDPOINT",
	"MEDIA_STORAGE_FORCE_PATH_STYLE": "false",
	"MEDIA_STORAGE_PROVIDER": "env-MEDIA_STORAGE_PROVIDER",
	"MEDIA_STORAGE_PUBLIC_ENDPOINT": "env-MEDIA_STORAGE_PUBLIC_ENDPOINT",
	"MEDIA_STORAGE_REGION": "env-MEDIA_STORAGE_REGION",
	"MEDIA_STORAGE_SECRET_KEY": "env-MEDIA_STORAGE_SECRET_KEY",
	"MEDIA_STREAM_MAX_SIZE_MB": "99",
	"MEDIA_TEMP_FILE_TTL": "9m",
	"MEDIA_UPLOAD_MAX_SIZE_MB": "99",
	"NATS_URL": "env-NATS_URL",
	"PRESIGN_DOWNLOAD_TTL": "9m",
	"PRESIGN_UPLOAD_TTL": "9m",
	"PREVIEW_MAX_SIZE_MB": "99",
	"REQUIRE_SCAN_BEFORE_READY": "false",
	"STORAGE_ACCESS_KEY": "env-STORAGE_ACCESS_KEY",
	"STORAGE_BUCKET": "env-STORAGE_BUCKET",
	"STORAGE_ENDPOINT": "env-STORAGE_ENDPOINT",
	"STORAGE_FORCE_PATH_STYLE": "false",
	"STORAGE_PROVIDER": "env-STORAGE_PROVIDER",
	"STORAGE_PUBLIC_ENDPOINT": "env-STORAGE_PUBLIC_ENDPOINT",
	"STORAGE_REGION": "env-STORAGE_REGION",
	"STORAGE_SECRET_KEY": "env-STORAGE_SECRET_KEY",
	"STREAM_MAX_SIZE_MB": "99",
	"TEMP_FILE_TTL": "9m",
	"UPLOAD_MAX_SIZE_MB": "99",
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
