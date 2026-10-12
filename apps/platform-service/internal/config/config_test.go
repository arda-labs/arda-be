package config

import "testing"

func TestParseTargets(t *testing.T) {
	got := ParseTargets(" ai-service=http://ai-service:8080 , platform-service=http://platform-service:8080,broken,=x")
	if len(got) != 2 || got["ai-service"] != "http://ai-service:8080" || got["platform-service"] != "http://platform-service:8080" {
		t.Fatalf("unexpected %v", got)
	}
}

// JOB_TARGET_URLS is optional: an unset or blank value must yield an empty
// (non-nil) map so the scheduler starts and reports targets as unconfigured.
func TestParseTargetsEmpty(t *testing.T) {
	for _, raw := range []string{"", "  ", ",", " , ", "=", "svc="} {
		got := ParseTargets(raw)
		if got == nil || len(got) != 0 {
			t.Fatalf("ParseTargets(%q) = %v, want empty map", raw, got)
		}
	}
}
