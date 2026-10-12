package service

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/migration"
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
// the real loan_outbox schema produced by this service's migrations.
func TestOutboxTableMatchesMigratedSchema(t *testing.T) {
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	var id string
	if err := db.QueryRow(`INSERT INTO loan_outbox (id, subject, event_code, dedupe_key, payload) VALUES ('evt-schema-test', 'arda.loan.disbursement.approved.v1', 'loan.disbursement.approved', 'schema-test', '{}') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	publisher := &recordingPublisher{}
	relay, err := outbox.New(db, publisher, loanOutboxTable, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	stats, err := relay.PublishOnce(context.Background())
	if err != nil || stats != (outbox.Stats{Claimed: 1, Published: 1}) {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if len(publisher.ids) != 1 || publisher.ids[0] != id || publisher.subjects[0] != "arda.loan.disbursement.approved.v1" {
		t.Fatalf("published ids=%v subjects=%v", publisher.ids, publisher.subjects)
	}
	var published bool
	if err := db.QueryRow(`SELECT published_at IS NOT NULL FROM loan_outbox WHERE id::text = $1`, id).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if !published {
		t.Fatal("row was not marked published")
	}
}
