package events

import (
	"testing"

	"github.com/nats-io/nats.go"
)

func TestNewConsumerRequiresExplicitBinding(t *testing.T) {
	if _, err := NewConsumer(nil, "arda.notification.inbox.created.v1", ""); err == nil {
		t.Fatal("NewConsumer accepted an empty durable name")
	}
	if _, err := NewConsumer(nil, "", "notification-inbox"); err == nil {
		t.Fatal("NewConsumer accepted an empty subject")
	}
}

func TestDLQMessageIDIsStableAndDerivedFromSourceID(t *testing.T) {
	msg := nats.NewMsg("arda.notification.inbox.created.v1")
	msg.Header.Set(nats.MsgIdHdr, "outbox-123")

	if got, want := dlqMessageID(msg), "arda-dlq:outbox-123"; got != want {
		t.Fatalf("dlq message ID = %q, want %q", got, want)
	}
	if got := dlqMessageID(msg); got != dlqMessageID(msg) {
		t.Fatalf("dlq message ID is not stable: %q", got)
	}
}

func TestDLQMessageIDIsStableWithoutSourceID(t *testing.T) {
	msg := nats.NewMsg("arda.notification.inbox.created.v1")
	msg.Data = []byte(`{"id":"event-123"}`)

	first := dlqMessageID(msg)
	if first == "" || first != dlqMessageID(msg) {
		t.Fatalf("fallback dlq message ID is not stable: %q", first)
	}
}
