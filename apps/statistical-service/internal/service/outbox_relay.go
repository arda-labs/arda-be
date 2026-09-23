package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// OutboxRelay publishes pending rpt_outbox_events rows to NATS JetStream and
// marks each published in the same pass. Rows stay pending on failure and are
// retried with attempt accounting, so a NATS outage delays an alert but never
// drops it — the alert row itself is already durable in rpt_indicator_alerts.
type OutboxRelay struct {
	db       *sql.DB
	js       nats.JetStreamContext
	interval time.Duration
	logger   *slog.Logger
}

// NewOutboxRelay returns nil when NATS is unavailable: the reporting ETL and
// the alert records must keep working without an event bus.
func NewOutboxRelay(db *sql.DB, conn *nats.Conn, logger *slog.Logger) *OutboxRelay {
	if conn == nil {
		return nil
	}
	js, err := conn.JetStream()
	if err != nil {
		logger.Error("outbox relay: jetstream context", "err", err)
		return nil
	}
	stream := "ARDA_EVENTS"
	if _, err := js.StreamInfo(stream); err != nil {
		if _, err := js.AddStream(&nats.StreamConfig{
			Name:     stream,
			Subjects: []string{"arda.>"},
			Storage:  nats.FileStorage,
		}); err != nil && err != nats.ErrStreamNameAlreadyInUse {
			logger.Error("outbox relay: add stream", "err", err)
			return nil
		}
	}
	return &OutboxRelay{db: db, js: js, interval: 2 * time.Second, logger: logger}
}

func (r *OutboxRelay) Run(ctx context.Context) {
	if r == nil {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.logger.Info("reporting outbox relay started", "interval", r.interval.String())
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("reporting outbox relay stopped")
			return
		case <-ticker.C:
			r.publishOnce(ctx)
		}
	}
}

func (r *OutboxRelay) publishOnce(ctx context.Context) {
	// Claim the batch in a transaction so two replicas (or a restart racing a
	// still-running relay) cannot publish the same row: SKIP LOCKED hands each
	// pending row to exactly one relay, and the lock is held until the row is
	// marked published. Nats-Msg-Id is the row id, so a republish after a
	// commit failure is deduplicated by JetStream.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		r.logger.Error("outbox relay: begin", "err", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
		SELECT id, subject, payload
		FROM rpt_outbox_events
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT 50
		FOR UPDATE SKIP LOCKED`)
	if err != nil {
		r.logger.Error("outbox relay: query", "err", err)
		return
	}
	type pending struct {
		id      string
		subject string
		payload []byte
	}
	batch := []pending{}
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.subject, &p.payload); err != nil {
			r.logger.Error("outbox relay: scan", "err", err)
			rows.Close()
			return
		}
		batch = append(batch, p)
	}
	if err := rows.Err(); err != nil {
		r.logger.Error("outbox relay: iterate", "err", err)
		rows.Close()
		return
	}
	rows.Close()

	for _, p := range batch {
		msg := &nats.Msg{Subject: p.subject, Data: p.payload, Header: nats.Header{}}
		msg.Header.Set(nats.MsgIdHdr, p.id)
		if _, err := r.js.PublishMsg(msg); err != nil {
			r.logger.Error("outbox relay: publish", "id", p.id, "subject", p.subject, "err", err)
			_, _ = tx.ExecContext(ctx, `
				UPDATE rpt_outbox_events
				SET publish_attempts = publish_attempts + 1, last_error = $2
				WHERE id = $1::uuid`, p.id, err.Error())
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE rpt_outbox_events SET published_at = now() WHERE id = $1::uuid`, p.id); err != nil {
			r.logger.Error("outbox relay: mark published", "id", p.id, "err", err)
		}
	}
	if err := tx.Commit(); err != nil {
		r.logger.Error("outbox relay: commit", "err", err)
	}
}
