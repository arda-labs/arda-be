package ardaevents

import (
	"testing"
	"time"
)

func TestNewEnvelopeSetsStableMetadata(t *testing.T) {
	payload := map[string]string{"key": "finance.default_currency"}
	env, err := NewEnvelope(EventPlatformParameterChanged, payload, Options{
		SourceService: "platform-service",
		TenantID:      "tenant-1",
		RequestID:     "req-1",
		OccurredAt:    time.Date(2026, 6, 27, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewEnvelope() error = %v", err)
	}
	if env.ID == "" {
		t.Fatal("expected generated id")
	}
	if env.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", env.SchemaVersion)
	}
	if env.EventCode != EventPlatformParameterChanged {
		t.Fatalf("event code = %q", env.EventCode)
	}
	if err := env.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestNewEnvelopeRequiresSourceService(t *testing.T) {
	_, err := NewEnvelope(EventPlatformLookupChanged, struct{}{}, Options{})
	if err == nil {
		t.Fatal("expected source service error")
	}
}
