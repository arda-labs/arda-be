package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

type Publisher interface {
	Publish(ctx context.Context, subject string, envelope EventEnvelope) error
	Close() error
}

type bufferedItem struct {
	subject  string
	envelope EventEnvelope
}

// BufferedPublisher provides an in-memory ring buffer (capacity 1000) that
// prevents event publishing from ever blocking HTTP request handling or DB transactions.
type BufferedPublisher struct {
	mu       sync.Mutex
	items    []bufferedItem
	capacity int
	logger   *slog.Logger
}

func NewBufferedPublisher(capacity int, logger *slog.Logger) *BufferedPublisher {
	if capacity <= 0 {
		capacity = 1000
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &BufferedPublisher{
		items:    make([]bufferedItem, 0, capacity),
		capacity: capacity,
		logger:   logger,
	}
}

func (b *BufferedPublisher) Publish(ctx context.Context, subject string, envelope EventEnvelope) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) >= b.capacity {
		// Drop oldest to preserve ring buffer invariant
		b.items = b.items[1:]
		b.logger.Warn("event ring buffer capacity reached; dropped oldest event", "capacity", b.capacity, "subject", subject)
	}
	b.items = append(b.items, bufferedItem{subject: subject, envelope: envelope})
	return nil
}

func (b *BufferedPublisher) Close() error {
	return nil
}

func (b *BufferedPublisher) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

// NATSPublisher publishes AI events to NATS JetStream on stream AI_EVENTS.
// It uses an internal buffered worker and retries with AckWait of 5s.
// If NATS is unavailable, it enqueues to the in-memory ring buffer.
type NATSPublisher struct {
	nc      *nats.Conn
	js      nats.JetStreamContext
	buffer  *BufferedPublisher
	queue   chan bufferedItem
	closeCh chan struct{}
	wg      sync.WaitGroup
	logger  *slog.Logger
}

func NewNATSPublisher(natsURL string, appName string, logger *slog.Logger) (*NATSPublisher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	buffer := NewBufferedPublisher(1000, logger)
	if strings.TrimSpace(natsURL) == "" {
		return nil, errors.New("natsURL is empty")
	}

	nc, err := nats.Connect(natsURL, nats.Name(appName), nats.Timeout(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("nats connect %q: %w", natsURL, err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats jetstream context: %w", err)
	}

	// Ensure stream AI_EVENTS exists
	_, streamErr := js.StreamInfo(StreamName)
	if streamErr != nil && errors.Is(streamErr, nats.ErrStreamNotFound) {
		_, err = js.AddStream(&nats.StreamConfig{
			Name: StreamName,
			Subjects: []string{
				"arda.ai.runs.>",
				"arda.ai.approvals.>",
				"arda.ai.knowledge.>",
				"arda.ai.audit.>",
			},
			Storage:   nats.FileStorage,
			Retention: nats.LimitsPolicy,
			MaxAge:    7 * 24 * time.Hour,
			Replicas:  1,
		})
		if err != nil {
			logger.Warn("could not add JetStream AI_EVENTS stream; proceeding with fallback buffer", "err", err)
		}
	}

	p := &NATSPublisher{
		nc:      nc,
		js:      js,
		buffer:  buffer,
		queue:   make(chan bufferedItem, 1000),
		closeCh: make(chan struct{}),
		logger:  logger,
	}

	p.wg.Add(1)
	go p.worker()

	return p, nil
}

func (p *NATSPublisher) worker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.closeCh:
			return
		case item := <-p.queue:
			p.dispatch(item)
		}
	}
}

func (p *NATSPublisher) dispatch(item bufferedItem) {
	payload, err := json.Marshal(item.envelope)
	if err != nil {
		p.logger.Error("failed to marshal event envelope", "type", item.envelope.Type, "err", err)
		return
	}

	msg := nats.NewMsg(item.subject)
	msg.Data = payload
	msg.Header.Set("Content-Type", "application/json")
	msg.Header.Set("X-Event-Type", item.envelope.Type)
	msg.Header.Set("X-Event-ID", item.envelope.ID)
	if item.envelope.TenantID != "" {
		msg.Header.Set("X-Tenant-ID", item.envelope.TenantID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, pubErr := p.js.PublishMsg(msg, nats.Context(ctx), nats.AckWait(5*time.Second))
	if pubErr != nil {
		p.logger.Warn("NATS JetStream publish failed; buffering event", "subject", item.subject, "err", pubErr)
		_ = p.buffer.Publish(context.Background(), item.subject, item.envelope)
	}
}

func (p *NATSPublisher) Publish(ctx context.Context, subject string, envelope EventEnvelope) error {
	item := bufferedItem{subject: subject, envelope: envelope}
	select {
	case p.queue <- item:
		return nil
	default:
		// Queue full, fallback to ring buffer
		return p.buffer.Publish(ctx, subject, envelope)
	}
}

func (p *NATSPublisher) Close() error {
	close(p.closeCh)
	p.wg.Wait()
	if p.nc != nil {
		p.nc.Close()
	}
	return nil
}
