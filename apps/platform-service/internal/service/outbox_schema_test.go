package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"testing"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-events/outbox"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	"github.com/nats-io/nats.go"
)

type recordingPublisher struct {
	subjects, ids []string
	fail          error
}

func (p *recordingPublisher) PublishMsg(msg *nats.Msg, _ ...nats.PubOpt) (*nats.PubAck, error) {
	if p.fail != nil {
		return nil, p.fail
	}
	p.subjects = append(p.subjects, msg.Subject)
	p.ids = append(p.ids, msg.Header.Get(nats.MsgIdHdr))
	return &nats.PubAck{Stream: outbox.StreamName}, nil
}

// TestOutboxTableMatchesMigratedSchema proves the relay configuration fits
// the real plt_outbox schema produced by this service's migrations: a UUID
// id becomes the Nats-Msg-Id, the subject column is honoured and the row is
// marked published.
func TestOutboxTableMatchesMigratedSchema(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	var id string
	if err := db.QueryRow(`INSERT INTO plt_outbox (subject, event_code, dedupe_key, payload) VALUES ($1, 'platform.job.failed', 'schema-test', '{}') RETURNING id::text`, domain.SubjectJobFailed).Scan(&id); err != nil {
		t.Fatal(err)
	}
	publisher := &recordingPublisher{}
	relay, err := outbox.New(db, publisher, platformOutboxTable, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	stats, err := relay.PublishOnce(context.Background())
	if err != nil || stats != (outbox.Stats{Claimed: 1, Published: 1}) {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if len(publisher.ids) != 1 || publisher.ids[0] != id || publisher.subjects[0] != domain.SubjectJobFailed {
		t.Fatalf("published ids=%v subjects=%v", publisher.ids, publisher.subjects)
	}
	var published bool
	if err := db.QueryRow(`SELECT published_at IS NOT NULL FROM plt_outbox WHERE id::text = $1`, id).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("row was not marked published")
	}
	if stats, _ := relay.PublishOnce(context.Background()); stats.Claimed != 0 {
		t.Fatalf("already published rows must not be re-sent, got %+v", stats)
	}
}

// A failed job run produces one plt_outbox row that the shared relay
// publishes exactly once.
func TestOutboxRelayPublishesJobFailure(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"failed"}`)
	_, _ = s.RunNow(context.Background(), "ai-eval", "u1", nil)
	s.Wait()
	publisher := &recordingPublisher{}
	relay, err := outbox.New(db, publisher, platformOutboxTable, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	stats, err := relay.PublishOnce(context.Background())
	if err != nil || stats.Published != 1 || len(publisher.subjects) != 1 || publisher.subjects[0] != domain.SubjectJobFailed {
		t.Fatalf("stats=%+v subjects=%v err=%v", stats, publisher.subjects, err)
	}
	if stats, _ := relay.PublishOnce(context.Background()); stats.Claimed != 0 {
		t.Fatalf("already published rows must not be re-sent, got %+v", stats)
	}
}

func TestOutboxRelayRecordsFailure(t *testing.T) {
	db, s := schedulerFixture(t, `{"status":"failed"}`)
	_, _ = s.RunNow(context.Background(), "ai-eval", "u1", nil)
	s.Wait()
	relay, err := outbox.New(db, &recordingPublisher{fail: errors.New("nats down")}, platformOutboxTable, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if stats, err := relay.PublishOnce(context.Background()); err != nil || stats.Failed != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	var attempts int
	var lastErr string
	if err := db.QueryRow(`SELECT publish_attempts, last_error FROM plt_outbox`).Scan(&attempts, &lastErr); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || lastErr != "nats down" {
		t.Fatalf("attempts=%d lastErr=%q", attempts, lastErr)
	}
}
