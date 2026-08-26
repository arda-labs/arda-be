package ardahttp

import (
	"errors"
	"testing"
)

func TestValidateEgressURL_AllowedPublicURLs(t *testing.T) {
	validURLs := []string{
		"https://api.openai.com/v1",
		"https://openrouter.ai/api/v1",
		"https://generativelanguage.googleapis.com/v1beta/openai",
		"https://api.anthropic.com/v1",
		"https://api.deepseek.com/v1",
	}

	for _, u := range validURLs {
		if err := ValidateEgressURL(u, false); err != nil {
			t.Errorf("expected '%s' to be valid, got: %v", u, err)
		}
	}
}

func TestValidateEgressURL_BlocksMetadataAndPrivateIPsInProduction(t *testing.T) {
	blockedURLs := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://0.0.0.0:8080/api",
		"http://127.0.0.1:11434/v1",
		"http://localhost:8080",
		"http://10.0.0.1/admin",
		"http://192.168.1.1/api",
		"http://172.16.0.5:9000",
		"ftp://example.com/file",
		"file:///etc/passwd",
	}

	for _, u := range blockedURLs {
		if err := ValidateEgressURL(u, false); err == nil {
			t.Errorf("expected '%s' to be blocked, but validation passed", u)
		}
	}
}

func TestValidateEgressURL_AllowsLocalWhenEnabled(t *testing.T) {
	localURLs := []string{
		"http://localhost:11434/v1",
		"http://127.0.0.1:8000/v1",
		"http://10.0.1.5:8080/v1",
	}

	for _, u := range localURLs {
		if err := ValidateEgressURL(u, true); err != nil {
			t.Errorf("expected '%s' to be allowed with allowLocal=true, got: %v", u, err)
		}
	}

	// Even with allowLocal=true, metadata services must ALWAYS be blocked
	if err := ValidateEgressURL("http://169.254.169.254/meta-data", true); !errors.Is(err, ErrBlockedHost) {
		t.Fatalf("metadata service must always be blocked even when allowLocal=true, got: %v", err)
	}
}
