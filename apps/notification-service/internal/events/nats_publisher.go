package events

import (
	"context"

	"github.com/nats-io/nats.go"
)

type NATSPublisher struct {
	conn *nats.Conn
}

func NewNATSPublisher(conn *nats.Conn) *NATSPublisher {
	return &NATSPublisher{conn: conn}
}

func (p *NATSPublisher) Publish(ctx context.Context, subject string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.conn.Publish(subject, payload); err != nil {
		return err
	}
	return p.conn.Flush()
}
