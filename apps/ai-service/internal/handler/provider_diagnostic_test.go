package handler

import (
	"errors"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
)

func TestProviderDiagnosticBoundsAndRedacts(t *testing.T) {
	long := "Authorization: Bearer super-secret-token " + strings.Repeat("x", 4096)
	err := &model.ProviderStatusError{StatusCode: 400, Body: long}

	got := providerDiagnostic(err)
	if got == "" {
		t.Fatal("provider diagnostic must not be empty for a provider status error")
	}
	if len([]rune(got)) > 1024 {
		t.Fatalf("provider diagnostic must be bounded, got %d runes", len([]rune(got)))
	}
	if strings.Contains(got, "super-secret-token") {
		t.Fatalf("provider diagnostic leaked a credential: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("provider diagnostic should keep the redaction marker: %q", got)
	}
}

func TestProviderDiagnosticIgnoresNonProviderErrors(t *testing.T) {
	if got := providerDiagnostic(errors.New("plain failure")); got != "" {
		t.Fatalf("non-provider error must not produce a body snippet, got %q", got)
	}
	if got := providerDiagnostic(nil); got != "" {
		t.Fatalf("nil error must not produce a body snippet, got %q", got)
	}
}
