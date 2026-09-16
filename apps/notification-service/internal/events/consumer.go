package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	consumerAckWait    = 30 * time.Second
	consumerMaxDeliver = 10

	// consumerRetryMin/Max bound the in-process read-retry backoff. A transient
	// NATS failure must not kill the consumer goroutine: Run keeps retrying and
	// only exits when the context is cancelled.
	consumerRetryMin = 500 * time.Millisecond
	consumerRetryMax = 30 * time.Second
)

// EventHandler must complete its local transaction and dedupe check before it
// returns nil. Only then does Consumer acknowledge the JetStream message.
type EventHandler func(context.Context, *nats.Msg) error

// messageSource is the slice of nats.Subscription the consumer loop needs, so
// tests can drive the retry behaviour without a NATS server.
type messageSource interface {
	NextMsgWithContext(ctx context.Context) (*nats.Msg, error)
	Unsubscribe() error
}

type Consumer struct {
	js      nats.JetStreamContext
	stream  string
	subject string
	durable string

	// retryMin/retryMax override the default backoff bounds (tests only).
	retryMin time.Duration
	retryMax time.Duration
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

// Run uses an explicit durable consumer shared across replicas via a queue
// group (all pods bind the same durable; only one is "bound" as a plain push
// subscriber, so a queue group is required for replicas > 1), manual
// acknowledgement, bounded redelivery and a stream-local DLQ subject. The
// handler remains responsible for an inbox/dedupe commit; NATS acknowledgement
// follows that commit.
//
// Transient failures (NATS read, dead-letter publish, ack) are logged and
// retried with backoff instead of terminating the goroutine, so a broker
// reconnect cannot silently stop consumption. Run returns only when the context
// is cancelled.
func (c *Consumer) Run(ctx context.Context, handler EventHandler) error {
	if c == nil || c.js == nil || handler == nil {
		return fmt.Errorf("consumer and handler are required")
	}
	sub, err := c.js.QueueSubscribeSync(c.subject, c.durable,
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

	return c.consume(ctx, sub, handler)
}

// consume is the retrying read loop shared by Run and its tests.
func (c *Consumer) consume(ctx context.Context, sub messageSource, handler EventHandler) error {
	backoff := c.retryFloor()
	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Error("event consumer read failed",
				"subject", c.subject,
				"durable", c.durable,
				"err", err,
				"retry_in", backoff,
			)
			if !sleepWithContext(ctx, backoff) {
				return ctx.Err()
			}
			backoff *= 2
			if backoff > c.retryCeiling() {
				backoff = c.retryCeiling()
			}
			continue
		}
		backoff = c.retryFloor()

		metadata, metadataErr := msg.Metadata()
		if metadataErr == nil && metadata.NumDelivered >= consumerMaxDeliver {
			if err := c.publishDLQ(ctx, msg); err != nil {
				// Do not ack: leaving the message unacknowledged keeps it
				// redeliverable once the dead-letter publish succeeds.
				slog.Error("publish dead-letter event failed",
					"subject", msg.Subject,
					"err", err,
				)
				continue
			}
			if err := msg.AckSync(nats.Context(ctx)); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				slog.Error("ack dead-lettered event failed", "subject", msg.Subject, "err", err)
			}
			continue
		}
		if err := handler(ctx, msg); err != nil {
			slog.Error("event handler failed",
				"subject", msg.Subject,
				"err", err,
			)
			if err := msg.NakWithDelay(consumerAckWait); err != nil {
				slog.Warn("negative ack failed", "subject", msg.Subject, "err", err)
			}
			continue
		}
		if err := msg.AckSync(nats.Context(ctx)); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Error("ack event failed", "subject", msg.Subject, "err", err)
		}
	}
}

func (c *Consumer) retryFloor() time.Duration {
	if c != nil && c.retryMin > 0 {
		return c.retryMin
	}
	return consumerRetryMin
}

func (c *Consumer) retryCeiling() time.Duration {
	if c != nil && c.retryMax > 0 {
		return c.retryMax
	}
	return consumerRetryMax
}

// sleepWithContext waits out the backoff and reports false when the context was
// cancelled first.
func sleepWithContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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
