package service

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	ardaevents "github.com/arda-labs/arda/libs/go/arda-events"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	"github.com/nats-io/nats.go"
)

type testOutboxPublisher struct {
	calls        atomic.Int32
	blockFirst   bool
	firstEntered chan struct{}
	releaseFirst chan struct{}
	failFirst    bool
	mu           sync.Mutex
	messageIDs   []string
	subjects     []string
	storedIDs    map[string]struct{}
}

func (p *testOutboxPublisher) PublishMsg(msg *nats.Msg, _ ...nats.PubOpt) (*nats.PubAck, error) {
	call := p.calls.Add(1)
	p.mu.Lock()
	messageID := msg.Header.Get(nats.MsgIdHdr)
	p.messageIDs = append(p.messageIDs, messageID)
	p.subjects = append(p.subjects, msg.Subject)
	if p.storedIDs == nil {
		p.storedIDs = make(map[string]struct{})
	}
	p.storedIDs[messageID] = struct{}{}
	p.mu.Unlock()
	if call == 1 && p.blockFirst {
		close(p.firstEntered)
		<-p.releaseFirst
	}
	if call == 1 && p.failFirst {
		return nil, context.DeadlineExceeded
	}
	return &nats.PubAck{Stream: "ARDA_EVENTS"}, nil
}

func TestOutboxRelayConcurrentReplicasPublishOnePendingRow(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	id := insertFinanceOutboxRow(t, db)
	publisher := &testOutboxPublisher{
		blockFirst:   true,
		firstEntered: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	logger := slog.New(slog.DiscardHandler)
	first := newOutboxRelay(db, publisher, logger)
	second := newOutboxRelay(db, publisher, logger)
	firstDone := make(chan struct{})
	go func() {
		first.publishOnce(context.Background())
		close(firstDone)
	}()
	select {
	case <-publisher.firstEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first relay did not publish the claimed event")
	}
	second.publishOnce(context.Background())
	close(publisher.releaseFirst)
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first relay did not finish")
	}

	if got := publisher.calls.Load(); got != 1 {
		t.Fatalf("publish calls = %d, want one across two replicas", got)
	}
	var publishedAt sql.NullTime
	if err := db.QueryRow(`SELECT published_at FROM fin_outbox ORDER BY created_at DESC LIMIT 1`).Scan(&publishedAt); err != nil {
		t.Fatal(err)
	}
	if !publishedAt.Valid {
		t.Fatal("event was not marked published after PubAck")
	}
	assertOutboxMessageIDs(t, publisher, 1, id)
	assertFinanceOutboxSubject(t, publisher)
}

func TestOutboxRelayRetryAfterPublishFailureUsesStableMessageID(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	id := insertFinanceOutboxRow(t, db)
	publisher := &testOutboxPublisher{failFirst: true}
	relay := newOutboxRelay(db, publisher, slog.New(slog.DiscardHandler))
	relay.publishOnce(context.Background())

	var publishedAt sql.NullTime
	var attempts int
	if err := db.QueryRow(`SELECT published_at, publish_attempts FROM fin_outbox WHERE id = $1`, id).Scan(&publishedAt, &attempts); err != nil {
		t.Fatal(err)
	}
	if publishedAt.Valid || attempts != 1 {
		t.Fatalf("after failed publish: published=%t attempts=%d, want false/1", publishedAt.Valid, attempts)
	}

	relay.publishOnce(context.Background())
	if err := db.QueryRow(`SELECT published_at, publish_attempts FROM fin_outbox WHERE id = $1`, id).Scan(&publishedAt, &attempts); err != nil {
		t.Fatal(err)
	}
	if !publishedAt.Valid || attempts != 1 {
		t.Fatalf("after successful retry: published=%t attempts=%d, want true/1", publishedAt.Valid, attempts)
	}
	assertOutboxMessageIDs(t, publisher, 2, id)
	assertFinanceOutboxSubject(t, publisher)
	publisher.mu.Lock()
	storedCount := len(publisher.storedIDs)
	publisher.mu.Unlock()
	if storedCount != 1 {
		t.Fatalf("JetStream accepted unique messages = %d, want one after redelivery", storedCount)
	}
}

func assertFinanceOutboxSubject(t *testing.T, publisher *testOutboxPublisher) {
	t.Helper()
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	for _, subject := range publisher.subjects {
		if subject != ardaevents.SubjectFinanceTransactionPosted {
			t.Fatalf("published subject = %q, want registry subject %q", subject, ardaevents.SubjectFinanceTransactionPosted)
		}
	}
}

func insertFinanceOutboxRow(t *testing.T, db *sql.DB) string {
	t.Helper()
	var id string
	err := db.QueryRow(`
		INSERT INTO fin_outbox (tenant_id, event_type, payload)
		VALUES ('tenant-outbox-test', 'finance.transaction.posted', '{"id":"test"}'::jsonb)
		RETURNING id::text`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertOutboxMessageIDs(t *testing.T, publisher *testOutboxPublisher, wantCount int, wantID string) {
	t.Helper()
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if len(publisher.messageIDs) != wantCount {
		t.Fatalf("captured message IDs = %d, want %d", len(publisher.messageIDs), wantCount)
	}
	for _, id := range publisher.messageIDs {
		if id != wantID {
			t.Fatalf("Nats-Msg-Id = %q, want outbox event id %q", id, wantID)
		}
		if id != publisher.messageIDs[0] {
			t.Fatalf("retry changed Nats-Msg-Id: %q != %q", id, publisher.messageIDs[0])
		}
	}
}
