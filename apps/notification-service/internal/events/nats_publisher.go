package events

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/nats-io/nats.go"
)

type NATSPublisher struct {
	js nats.JetStreamContext
}

const eventStreamName = "ARDA_EVENTS"

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

func (p *NATSPublisher) Publish(ctx context.Context, event domain.OutboxEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.js == nil {
		return fmt.Errorf("jetstream publisher is not configured")
	}
	msg := nats.NewMsg(event.Subject)
	msg.Data = event.Payload
	// JetStream uses this header for server-side duplicate suppression during
	// the outbox retry window. The database row remains the source of truth.
	msg.Header.Set(nats.MsgIdHdr, event.ID)
	if _, err := p.js.PublishMsg(msg, nats.Context(ctx)); err != nil {
		return fmt.Errorf("publish event %s: %w", event.ID, err)
	}
	return nil
}
