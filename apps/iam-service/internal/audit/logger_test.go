package audit

import (
	"context"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

type recordingWriter struct {
	event *domain.AuthEvent
	ctx   context.Context
}

func (w *recordingWriter) InsertWithChain(ctx context.Context, event *domain.AuthEvent) error {
	w.ctx = ctx
	w.event = event
	return nil
}

func TestEventRedactsSensitiveDetailsRecursively(t *testing.T) {
	event := &domain.AuthEvent{Details: map[string]any{
		"access_token": "secret-token",
		"nested": map[string]any{
			"password":    "secret-password",
			"resource_id": "safe-id",
		},
		"items": []any{map[string]any{"cookie": "session-cookie"}},
	}}

	logger := New("test", nil)
	logger.Event(context.Background(), event)

	if event.Details["access_token"] != "[REDACTED]" {
		t.Fatalf("access token was not redacted: %#v", event.Details["access_token"])
	}
	nested := event.Details["nested"].(map[string]any)
	if nested["password"] != "[REDACTED]" || nested["resource_id"] != "safe-id" {
		t.Fatalf("nested details were not redacted safely: %#v", nested)
	}
	items := event.Details["items"].([]any)
	if items[0].(map[string]any)["cookie"] != "[REDACTED]" {
		t.Fatalf("array details were not redacted: %#v", items)
	}
}

func TestEventPersistsSynchronouslyWithRequestContext(t *testing.T) {
	writer := &recordingWriter{}
	logger := New("test", writer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	event := &domain.AuthEvent{EventType: "permission_denied", Result: "denied"}

	logger.Event(ctx, event)

	if writer.event == nil {
		t.Fatal("audit event was not persisted before Event returned")
	}
	if writer.ctx != ctx {
		t.Fatal("request context was not passed to the audit writer")
	}
	if writer.event.Timestamp.IsZero() || writer.event.ServiceName != "test" {
		t.Fatalf("event metadata was not populated: %#v", writer.event)
	}
}

func TestEventDoesNotDependOnBackgroundScheduling(t *testing.T) {
	writer := &recordingWriter{}
	logger := New("test", writer)
	logger.Event(context.Background(), &domain.AuthEvent{
		EventType: "session_revoked",
		Result:    "success",
		Details:   map[string]any{"at": time.Now()},
	})
	if writer.event == nil {
		t.Fatal("synchronous audit persistence regressed")
	}
}
