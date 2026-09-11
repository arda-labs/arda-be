package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// OutboxRelay publishes pending fin_outbox rows to NATS JetStream, marking
// each row published in the same update. Rows stay pending on failure and
// are retried on the next tick with exponential attempt accounting.
type OutboxRelay struct {
	db       *sql.DB
	js       nats.JetStreamContext
	interval time.Duration
	logger   *slog.Logger
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
	return &OutboxRelay{db: db, js: js, interval: 2 * time.Second, logger: logger}
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
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, aggregate_id, payload, publish_attempts
		FROM fin_outbox
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT 50`)
	if err != nil {
		r.logger.Error("outbox relay: query", "err", err)
		return
	}
	type pending struct {
		id       string
		tenantID string
		aggID    string
		payload  []byte
		attempts int
	}
	defer rows.Close()
	var batch []pending
	for rows.Next() {
		var p pending
		var aggID *string
		if err := rows.Scan(&p.id, &p.tenantID, &aggID, &p.payload, &p.attempts); err != nil {
			r.logger.Error("outbox relay: scan", "err", err)
			return
		}
		if aggID != nil {
			p.aggID = *aggID
		}
		batch = append(batch, p)
	}
	rows.Close()

	for _, p := range batch {
		subject := "arda.finance.journal.posted.v1"
		if _, err := r.js.Publish(subject, p.payload); err != nil {
			r.logger.Error("outbox relay: publish", "id", p.id, "err", err)
			_, _ = r.db.ExecContext(ctx, `
				UPDATE fin_outbox SET publish_attempts = publish_attempts + 1, last_error = $2
				WHERE id = $1`, p.id, err.Error())
			continue
		}
		if _, err := r.db.ExecContext(ctx, `
			UPDATE fin_outbox SET published_at = now() WHERE id = $1`, p.id); err != nil {
			r.logger.Error("outbox relay: mark published", "id", p.id, "err", err)
		}
	}
}
