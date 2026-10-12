package service

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"github.com/arda-labs/arda/apps/statistical-service/internal/migration"
	"github.com/arda-labs/arda/libs/go/arda-events/outbox"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	"github.com/nats-io/nats.go"
)

type recordingPublisher struct{ subjects, ids []string }

func (p *recordingPublisher) PublishMsg(msg *nats.Msg, _ ...nats.PubOpt) (*nats.PubAck, error) {
	p.subjects = append(p.subjects, msg.Subject)
	p.ids = append(p.ids, msg.Header.Get(nats.MsgIdHdr))
	return &nats.PubAck{Stream: outbox.StreamName}, nil
}

// TestOutboxTableMatchesMigratedSchema proves the relay configuration fits
// the real rpt_outbox_events schema produced by this service's migrations.
func TestOutboxTableMatchesMigratedSchema(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	var id string
	if err := db.QueryRow(`INSERT INTO rpt_outbox_events (tenant_id, subject, dedupe_key, payload) VALUES ('tenant-schema-test', 'arda.statistical.indicator.breached.v1', 'schema-test', '{}') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	publisher := &recordingPublisher{}
	relay, err := outbox.New(db, publisher, reportingOutboxTable, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	stats, err := relay.PublishOnce(context.Background())
	if err != nil || stats != (outbox.Stats{Claimed: 1, Published: 1}) {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if len(publisher.ids) != 1 || publisher.ids[0] != id || publisher.subjects[0] != "arda.statistical.indicator.breached.v1" {
		t.Fatalf("published ids=%v subjects=%v", publisher.ids, publisher.subjects)
	}
	var published bool
	if err := db.QueryRow(`SELECT published_at IS NOT NULL FROM rpt_outbox_events WHERE id::text = $1`, id).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("row was not marked published")
	}
}
