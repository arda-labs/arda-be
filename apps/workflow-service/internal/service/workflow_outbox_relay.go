package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

type workflowOutboxPublisher interface {
	PublishMsg(*nats.Msg, ...nats.PubOpt) (*nats.PubAck, error)
}

type WorkflowOutboxRelay struct {
	db       *sql.DB
	js       workflowOutboxPublisher
	logger   *slog.Logger
	interval time.Duration
}

func NewWorkflowOutboxRelay(db *sql.DB, conn *nats.Conn, logger *slog.Logger) *WorkflowOutboxRelay {
	if conn == nil {
		return nil
	}
	js, err := conn.JetStream()
	if err != nil {
		logger.Error("workflow outbox: jetstream context", "err", err)
		return nil
	}
	if _, err := js.StreamInfo("ARDA_EVENTS"); err != nil {
		if _, err := js.AddStream(&nats.StreamConfig{Name: "ARDA_EVENTS", Subjects: []string{"arda.>"}, Storage: nats.FileStorage}); err != nil && err != nats.ErrStreamNameAlreadyInUse {
			logger.Error("workflow outbox: add stream", "err", err)
			return nil
		}
	}
	return &WorkflowOutboxRelay{db: db, js: js, logger: logger, interval: 2 * time.Second}
}

func (r *WorkflowOutboxRelay) Run(ctx context.Context) {
	if r == nil {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.logger.Info("workflow outbox relay started", "interval", r.interval.String())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.publishOnce(ctx)
		}
	}
}

func (r *WorkflowOutboxRelay) publishOnce(ctx context.Context) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		r.logger.Error("workflow outbox: begin", "err", err)
		return
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id, subject, payload FROM workflow_outbox WHERE published_at IS NULL ORDER BY created_at, id LIMIT 50 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		r.logger.Error("workflow outbox: query", "err", err)
		return
	}
	type pending struct {
		id, subject string
		payload     []byte
	}
	var batch []pending
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.subject, &item.payload); err != nil {
			rows.Close()
			r.logger.Error("workflow outbox: scan", "err", err)
			return
		}
		batch = append(batch, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		r.logger.Error("workflow outbox: iterate", "err", err)
		return
	}
	rows.Close()
	for _, item := range batch {
		msg := &nats.Msg{Subject: item.subject, Data: item.payload, Header: nats.Header{}}
		msg.Header.Set(nats.MsgIdHdr, item.id)
		if _, err := r.js.PublishMsg(msg, nats.Context(ctx)); err != nil {
			r.logger.Error("workflow outbox: publish", "id", item.id, "err", err)
			_, _ = tx.ExecContext(ctx, `UPDATE workflow_outbox SET publish_attempts = publish_attempts + 1, last_error = $2 WHERE id = $1`, item.id, err.Error())
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_outbox SET published_at = now(), last_error = NULL WHERE id = $1`, item.id); err != nil {
			r.logger.Error("workflow outbox: mark published", "id", item.id, "err", err)
		}
	}
	if err := tx.Commit(); err != nil {
		r.logger.Error("workflow outbox: commit", "err", err)
	}
}
