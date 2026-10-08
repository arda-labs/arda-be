package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	ardaevents "github.com/arda-labs/arda/libs/go/arda-events"
	"github.com/nats-io/nats.go"
)

// OutboxRelay publishes pending fin_outbox rows to NATS JetStream, marking
// each row published in the same update. Rows stay pending on failure and
// are retried on the next tick with exponential attempt accounting.
type OutboxRelay struct {
	db       *sql.DB
	js       outboxPublisher
	interval time.Duration
	logger   *slog.Logger
}

type outboxPublisher interface {
	PublishMsg(*nats.Msg, ...nats.PubOpt) (*nats.PubAck, error)
}

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
	return newOutboxRelay(db, js, logger)
}

func newOutboxRelay(db *sql.DB, publisher outboxPublisher, logger *slog.Logger) *OutboxRelay {
	return &OutboxRelay{db: db, js: publisher, interval: 2 * time.Second, logger: logger}
}

func (r *OutboxRelay) Run(ctx context.Context) {
	if r == nil {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.logger.Info("outbox relay started", "interval", r.interval.String())
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("outbox relay stopped")
			return
		case <-ticker.C:
			r.publishOnce(ctx)
		}
	}
}

func (r *OutboxRelay) publishOnce(ctx context.Context) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		r.logger.Error("outbox relay: begin", "err", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `
		SELECT id, payload
		FROM fin_outbox
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
		payload []byte
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.payload); err != nil {
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
		msg := &nats.Msg{Subject: ardaevents.SubjectFinanceTransactionPosted, Data: p.payload, Header: nats.Header{}}
		msg.Header.Set(nats.MsgIdHdr, p.id)
		if _, err := r.js.PublishMsg(msg, nats.Context(ctx)); err != nil {
			r.logger.Error("outbox relay: publish", "id", p.id, "err", err)
			if _, updateErr := tx.ExecContext(ctx, `
				UPDATE fin_outbox SET publish_attempts = publish_attempts + 1, last_error = $2
				WHERE id = $1`, p.id, err.Error()); updateErr != nil {
				r.logger.Error("outbox relay: record failure", "id", p.id, "err", updateErr)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE fin_outbox SET published_at = now(), last_error = NULL WHERE id = $1`, p.id); err != nil {
			r.logger.Error("outbox relay: mark published", "id", p.id, "err", err)
		}
	}
	if err := tx.Commit(); err != nil {
		r.logger.Error("outbox relay: commit", "err", err)
	}
}
