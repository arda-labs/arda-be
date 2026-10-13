package config

import (
	"path/filepath"
	"testing"
)

func TestLoadJobTargetURLs(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("JOB_TARGET_URLS", " ai-service=http://ai-service:8080 , platform-service=http://platform-service:8080")
	got := Load().JobTargetURLs
	if len(got) != 2 || got["ai-service"] != "http://ai-service:8080" || got["platform-service"] != "http://platform-service:8080" {
		t.Fatalf("unexpected %v", got)
	}
}

// JOB_TARGET_URLS is optional: an unset or blank value must yield an empty
// (non-nil) map so the scheduler starts and reports targets as unconfigured.
func TestLoadJobTargetURLsEmpty(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	for _, raw := range []string{"", "  ", ","} {
		t.Setenv("JOB_TARGET_URLS", raw)
		got := Load().JobTargetURLs
		if got == nil || len(got) != 0 {
			t.Fatalf("JOB_TARGET_URLS=%q: got %v, want empty map", raw, got)
		}
	}
}

// Malformed pairs are reported on stderr but never block the valid ones.
func TestLoadJobTargetURLsKeepsValidPairs(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "missing.yaml"))
	t.Setenv("JOB_TARGET_URLS", "ai-service=http://ai-service:8080,broken,=x")
	got := Load().JobTargetURLs
	if len(got) != 1 || got["ai-service"] != "http://ai-service:8080" {
		t.Fatalf("unexpected %v", got)
	}
}
