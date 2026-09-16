package events

import (
	"context"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go"
)

// eventStreamName is the shared durable JetStream stream every service
// publishes into (see notification-service and finance-service).
const eventStreamName = "ARDA_EVENTS"

// NATSPublisher publishes outbox payloads to JetStream. The outbox row id is
// set as Nats-Msg-Id so JetStream suppresses duplicate publishes during the
// retry window; the database row stays the source of truth.
type NATSPublisher struct {
	js nats.JetStreamContext
}

func NewNATSPublisher(conn *nats.Conn) (*NATSPublisher, error) {
	if conn == nil {
		return nil, fmt.Errorf("nats connection is required")
	}
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}
	if _, err := js.StreamInfo(eventStreamName); err != nil {
		if err != nats.ErrStreamNotFound {
			return nil, fmt.Errorf("inspect event stream: %w", err)
		}
		if _, err := js.AddStream(&nats.StreamConfig{
			Name:      eventStreamName,
			Subjects:  []string{"arda.>"},
			Storage:   nats.FileStorage,
			Retention: nats.LimitsPolicy,
		}); err != nil {
			return nil, fmt.Errorf("create event stream: %w", err)
		}
	}
	return &NATSPublisher{js: js}, nil
}

func (p *NATSPublisher) Publish(ctx context.Context, subject, eventID string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.js == nil {
		return fmt.Errorf("jetstream publisher is not configured")
	}
	msg := nats.NewMsg(subject)
	msg.Data = payload
	if eventID != "" {
		msg.Header.Set(nats.MsgIdHdr, eventID)
	}
	if _, err := p.js.PublishMsg(msg, nats.Context(ctx)); err != nil {
		return fmt.Errorf("publish event %s: %w", eventID, err)
	}
	return nil
}

// SubjectForEventType maps a media outbox event_type to its NATS subject, for
// example media.upload.completed -> arda.media.upload.completed.
func SubjectForEventType(eventType string) string {
	return "arda." + strings.TrimSpace(eventType)
}
