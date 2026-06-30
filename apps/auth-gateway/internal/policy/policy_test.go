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

func writePolicy(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
