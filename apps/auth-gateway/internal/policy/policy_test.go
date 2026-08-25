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
