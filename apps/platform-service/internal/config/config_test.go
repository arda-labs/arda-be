package config

import "testing"

func TestParseTargets(t *testing.T) {
	got := ParseTargets(" ai-service=http://ai-service:8080 , platform-service=http://platform-service:8080,broken,=x")
	if len(got) != 2 || got["ai-service"] != "http://ai-service:8080" || got["platform-service"] != "http://platform-service:8080" {
		t.Fatalf("unexpected %v", got)
	}
}
