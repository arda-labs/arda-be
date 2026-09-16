package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

// fakeMessageSource scripts NextMsgWithContext results for the retry loop and
// blocks on the context once the script is exhausted.
type fakeMessageSource struct {
	mu    sync.Mutex
	calls int
	steps []func(context.Context) (*nats.Msg, error)
}

func (f *fakeMessageSource) NextMsgWithContext(ctx context.Context) (*nats.Msg, error) {
	f.mu.Lock()
	index := f.calls
	f.calls++
	f.mu.Unlock()

	if index < len(f.steps) {
		return f.steps[index](ctx)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeMessageSource) Unsubscribe() error { return nil }

func (f *fakeMessageSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func fastConsumer() *Consumer {
	return &Consumer{
		subject:  "arda.notification.test.v1",
		durable:  "notification-test-cg",
		retryMin: time.Millisecond,
		retryMax: 2 * time.Millisecond,
	}
}

// TestConsumerRetriesTransientReadErrors proves a read failure no longer kills
// the loop: it backs off, reads again and only stops when the context ends.
func TestConsumerRetriesTransientReadErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	readErr := errors.New("nats: temporary failure")
	failures := 0
	src := &fakeMessageSource{steps: []func(context.Context) (*nats.Msg, error){
		func(context.Context) (*nats.Msg, error) { failures++; return nil, readErr },
		func(context.Context) (*nats.Msg, error) { failures++; return nil, readErr },
		func(context.Context) (*nats.Msg, error) { cancel(); return nil, context.Canceled },
	}}

	handlerCalls := 0
	err := fastConsumer().consume(ctx, src, func(context.Context, *nats.Msg) error {
		handlerCalls++
		return nil
	})

	if failures != 2 {
		t.Fatalf("read failures = %d, want the loop to read again after each failure", failures)
	}
	if handlerCalls != 0 {
		t.Fatalf("handler calls = %d, want 0", handlerCalls)
	}
	if src.callCount() != 3 {
		t.Fatalf("read attempts = %d, want 3", src.callCount())
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consume = %v, want context.Canceled on shutdown", err)
	}
}

// TestConsumerKeepsRunningAfterHandlerError proves a failing handler is logged,
// left unacknowledged (redelivery) and does not terminate consumption.
func TestConsumerKeepsRunningAfterHandlerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handlerErr := errors.New("handler commit failed")
	handlerCalls := 0
	src := &fakeMessageSource{steps: []func(context.Context) (*nats.Msg, error){
		func(context.Context) (*nats.Msg, error) {
			return nats.NewMsg("arda.notification.test.v1"), nil
		},
		func(context.Context) (*nats.Msg, error) { cancel(); return nil, context.Canceled },
	}}

	err := fastConsumer().consume(ctx, src, func(context.Context, *nats.Msg) error {
		handlerCalls++
		return handlerErr
	})

	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("consume = %v, want the loop to keep running until cancellation", err)
	}
	if src.callCount() != 2 {
		t.Fatalf("read attempts = %d, want 2 (position advanced after the handler error)", src.callCount())
	}
}
