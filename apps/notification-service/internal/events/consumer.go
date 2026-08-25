package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	consumerAckWait    = 30 * time.Second
	consumerMaxDeliver = 10
)

// EventHandler must complete its local transaction and dedupe check before it
// returns nil. Only then does Consumer acknowledge the JetStream message.
type EventHandler func(context.Context, *nats.Msg) error

type Consumer struct {
	js      nats.JetStreamContext
	stream  string
	subject string
	durable string
}

func NewConsumer(conn *nats.Conn, subject, durable string) (*Consumer, error) {
	if conn == nil {
		return nil, fmt.Errorf("nats connection is required")
	}
	subject = strings.TrimSpace(subject)
	durable = strings.TrimSpace(durable)
	if subject == "" || durable == "" {
		return nil, fmt.Errorf("consumer subject and durable name are required")
	}
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("create jetstream context: %w", err)
	}
	return &Consumer{js: js, stream: eventStreamName, subject: subject, durable: durable}, nil
}

// Run uses an explicit durable consumer, manual acknowledgement, bounded
// redelivery and a stream-local DLQ subject. The handler remains responsible
// for an inbox/dedupe commit; NATS acknowledgement follows that commit.
func (c *Consumer) Run(ctx context.Context, handler EventHandler) error {
	if c == nil || c.js == nil || handler == nil {
		return fmt.Errorf("consumer and handler are required")
	}
	sub, err := c.js.SubscribeSync(c.subject,
		nats.Durable(c.durable),
		nats.ManualAck(),
		nats.AckWait(consumerAckWait),
		nats.MaxDeliver(consumerMaxDeliver),
		nats.BindStream(c.stream),
	)
	if err != nil {
		return fmt.Errorf("subscribe durable consumer: %w", err)
	}
	defer sub.Unsubscribe()

	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read event: %w", err)
		}
		metadata, metadataErr := msg.Metadata()
		if metadataErr == nil && metadata.NumDelivered >= consumerMaxDeliver {
			if err := c.publishDLQ(ctx, msg); err != nil {
				return err
			}
			if err := msg.AckSync(nats.Context(ctx)); err != nil {
				return fmt.Errorf("ack dead-lettered event: %w", err)
			}
			continue
		}
		if err := handler(ctx, msg); err != nil {
			_ = msg.NakWithDelay(consumerAckWait)
			continue
		}
		if err := msg.AckSync(nats.Context(ctx)); err != nil {
			return fmt.Errorf("ack event: %w", err)
		}
	}
}

func (c *Consumer) publishDLQ(ctx context.Context, msg *nats.Msg) error {
	dlq := nats.NewMsg("arda.dlq." + msg.Subject)
	dlq.Data = msg.Data
	dlq.Header = nats.Header{}
	for key, values := range msg.Header {
		dlq.Header[key] = append([]string(nil), values...)
	}
	dlq.Header.Set("Arda-DLQ-Reason", "max_delivery_attempts")
	dlq.Header.Set("Arda-DLQ-Source", msg.Subject)
	// Ack can be interrupted after the DLQ publish. Reusing a stable message
	// ID lets JetStream suppress that duplicate on the retry path.
	dlq.Header.Set(nats.MsgIdHdr, dlqMessageID(msg))
	if _, err := c.js.PublishMsg(dlq, nats.Context(ctx)); err != nil {
		return fmt.Errorf("publish dead-letter event: %w", err)
	}
	return nil
}

func dlqMessageID(msg *nats.Msg) string {
	if msg == nil {
		return "arda-dlq:empty"
	}
	if sourceID := strings.TrimSpace(msg.Header.Get(nats.MsgIdHdr)); sourceID != "" {
		return "arda-dlq:" + sourceID
	}
	if metadata, err := msg.Metadata(); err == nil && metadata.Sequence.Stream > 0 {
		return fmt.Sprintf("arda-dlq:%s:%d", metadata.Stream, metadata.Sequence.Stream)
	}
	hash := sha256.Sum256(append([]byte(msg.Subject+":"), msg.Data...))
	return "arda-dlq:" + hex.EncodeToString(hash[:])
}
