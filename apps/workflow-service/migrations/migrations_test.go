package migrations

import (
	"strings"
	"testing"
)

func TestCanaryMigrationScriptsAreNotEmbeddedAtStartup(t *testing.T) {
	entries, err := FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "_canary_") {
			t.Errorf("canary route script %q must stay outside startup migrations", entry.Name())
		}
	}
}
