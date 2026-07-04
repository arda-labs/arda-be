package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsYAMLAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(`app_name: hrm-test
http_addr: 127.0.0.1:9999
log_level: debug
database_dsn: "postgres://yaml"
workflow_grpc_addr: "127.0.0.1:9093"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("CONFIG_FILE", path)
	t.Setenv("HTTP_ADDR", "0.0.0.0:8097")

	cfg := Load()
	if cfg.AppName != "hrm-test" {
		t.Fatalf("AppName = %q", cfg.AppName)
	}
	if cfg.HTTPAddr != "0.0.0.0:8097" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.WorkflowGRPCAddr != "127.0.0.1:9093" {
		t.Fatalf("WorkflowGRPCAddr = %q", cfg.WorkflowGRPCAddr)
	}
}
