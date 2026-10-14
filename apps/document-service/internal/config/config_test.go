package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	for _, k := range []string{"APP_NAME", "HTTP_ADDR", "LOG_LEVEL", "DOCUMENT_MODEL_PRESET", "DOCUMENT_MODEL_BASE_URL",
		"DOCUMENT_MODEL_NAME", "DOCUMENT_MODEL_API_KEY", "DOCUMENT_MODEL_TIMEOUT", "DOCUMENT_MAX_IMAGE_SIDE",
		"DOCUMENT_MAX_IMAGE_BYTES", "DOCUMENT_REQUIRE_LOCAL", "DOCUMENT_LOCAL_HOSTS"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestLoadDefaults(t *testing.T) {
	isolate(t)
	cfg := Load()
	if cfg.AppName != "document-service" || cfg.HTTPAddr != "0.0.0.0:8100" {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.RequestTimeout != 25*time.Second || cfg.MaxImageSide != 1600 || cfg.MaxImageBytes != 10<<20 {
		t.Fatalf("limits = %+v", cfg)
	}
	if cfg.ModelPreset != "openai-compatible" || cfg.RequireLocal {
		t.Fatalf("model defaults = %+v", cfg)
	}
}

func TestLoadYAMLThenEnvWins(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte("model:\n  preset: opencode-go\n  base_url: https://yaml.example/v1\n  name: from-yaml\n  timeout: 12s\nprivacy:\n  require_local: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("DOCUMENT_MODEL_NAME", "from-env")
	t.Setenv("DOCUMENT_MODEL_API_KEY", "k")
	cfg := Load()
	if cfg.ModelPreset != "opencode-go" || cfg.ModelBaseURL != "https://yaml.example/v1" || cfg.RequestTimeout != 12*time.Second {
		t.Fatalf("yaml not applied: %+v", cfg)
	}
	if cfg.ModelName != "from-env" || cfg.ModelAPIKey != "k" || !cfg.RequireLocal {
		t.Fatalf("env not applied: %+v", cfg)
	}
}

func TestModelLocation(t *testing.T) {
	hosts := []string{"*.svc.cluster.local", "localhost", "127.0.0.1", "192.168.10.0/24"}
	cases := map[string]string{
		"http://ocr-model.arda-app.svc.cluster.local:8080/v1": LocationLocal,
		"http://localhost:8081/v1":                            LocationLocal,
		"http://192.168.10.205:8000/v1":                       LocationLocal,
		"https://opencode.ai/zen/go/v1":                       LocationCloud,
		"https://evilsvc.cluster.local/v1":                    LocationCloud,
		"http://192.168.11.5/v1":                              LocationCloud,
		"":                                                    LocationCloud,
	}
	for base, want := range cases {
		cfg := Config{ModelBaseURL: base, LocalHosts: hosts}
		if got := cfg.ModelLocation(); got != want {
			t.Errorf("%q => %s, want %s", base, got, want)
		}
	}
}

func TestValidateRequireLocalRejectsCloud(t *testing.T) {
	cfg := Config{ModelPreset: "opencode-go", ModelBaseURL: "https://opencode.ai/zen/go/v1", RequireLocal: true,
		RequestTimeout: time.Second, MaxImageSide: 1600, MaxImageBytes: 1 << 20, LocalHosts: []string{"localhost"}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("cloud model accepted while require_local is set")
	}
	cfg.ModelBaseURL = "http://localhost:8081/v1"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("local model rejected: %v", err)
	}
	cfg.ModelBaseURL = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled model rejected: %v", err)
	}
}

func TestValidateRejectsUnknownPreset(t *testing.T) {
	cfg := Config{ModelPreset: "mystery", RequestTimeout: time.Second, MaxImageSide: 1600, MaxImageBytes: 1}
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown preset accepted")
	}
}

func TestModelConfigured(t *testing.T) {
	hosts := []string{"localhost"}
	cases := []struct {
		cfg  Config
		want bool
	}{
		{Config{ModelBaseURL: "https://opencode.ai/zen/go/v1", ModelName: "m", ModelAPIKey: "k", LocalHosts: hosts}, true},
		{Config{ModelBaseURL: "https://opencode.ai/zen/go/v1", ModelName: "m", LocalHosts: hosts}, false},
		{Config{ModelBaseURL: "http://localhost:8081/v1", ModelName: "m", LocalHosts: hosts}, true},
		{Config{ModelBaseURL: "", ModelName: "m", ModelAPIKey: "k", LocalHosts: hosts}, false},
		{Config{ModelBaseURL: "https://opencode.ai/zen/go/v1", ModelAPIKey: "k", LocalHosts: hosts}, false},
	}
	for i, c := range cases {
		if got := c.cfg.ModelConfigured(); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}
