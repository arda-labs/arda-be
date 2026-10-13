package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// OutboxRelay publishes pending plt_outbox rows and marks them published.
type OutboxRelay struct {
	db       *sql.DB
	publish  func(subject, id string, data []byte) error
	logger   *slog.Logger
	interval time.Duration
}

func NewOutboxRelay(db *sql.DB, publish func(subject, id string, data []byte) error, logger *slog.Logger) *OutboxRelay {
	return &OutboxRelay{db: db, publish: publish, logger: logger, interval: 2 * time.Second}
}

// Run polls the outbox until ctx is cancelled.
func (r *OutboxRelay) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := r.PublishOnce(ctx); err != nil {
				r.logger.Error("platform outbox relay", "err", err)
			}
		}
	}
}

// PublishOnce sends one batch of unpublished rows. Rows are locked with SKIP
// LOCKED so replicas do not double-send; a failed publish only bumps
// publish_attempts/last_error and the row is retried on the next pass.
func (r *OutboxRelay) PublishOnce(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id::text, subject, payload FROM plt_outbox
		WHERE published_at IS NULL ORDER BY created_at, id LIMIT 50 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id, subject string
		payload     []byte
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.subject, &p.payload); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	sent := 0
	for _, p := range batch {
		if err := r.publish(p.subject, p.id, p.payload); err != nil {
			if _, uerr := tx.ExecContext(ctx, `UPDATE plt_outbox SET publish_attempts = publish_attempts + 1, last_error = $2 WHERE id = $1::uuid`, p.id, err.Error()); uerr != nil {
				return sent, uerr
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE plt_outbox SET published_at = now() WHERE id = $1::uuid`, p.id); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, tx.Commit()
}
