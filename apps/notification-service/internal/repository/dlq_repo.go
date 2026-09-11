package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// OutboxDLQEntry is one dead-lettered outbox event for the admin DLQ view.
type OutboxDLQEntry struct {
	OutboxID       string          `json:"outbox_id"`
	TenantID       string          `json:"tenant_id"`
	Subject        string          `json:"subject"`
	EventCode      string          `json:"event_code"`
	Payload        json.RawMessage `json:"payload"`
	Attempts       int             `json:"attempts"`
	LastError      string          `json:"last_error"`
	DeadLetteredAt time.Time       `json:"dead_lettered_at"`
	ReplayedAt     *time.Time      `json:"replayed_at,omitempty"`
}

// ListOutboxDLQ returns pending (not replayed) dead-lettered events for the
// tenant, newest first.
func (r *NotificationRepository) ListOutboxDLQ(ctx context.Context, tenantID string, limit int) ([]OutboxDLQEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT d.outbox_id, d.tenant_id, d.subject, COALESCE(o.event_code, ''),
		       d.payload, d.attempts, d.last_error, d.dead_lettered_at, d.replayed_at
		FROM notification_outbox_dlq d
		LEFT JOIN noti_outbox o ON o.id = d.outbox_id
		WHERE d.tenant_id = $1 AND d.replayed_at IS NULL
		ORDER BY d.dead_lettered_at DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list outbox dlq: %w", err)
	}
	defer rows.Close()

	items := []OutboxDLQEntry{}
	for rows.Next() {
		var it OutboxDLQEntry
		var payload []byte
		if err := rows.Scan(&it.OutboxID, &it.TenantID, &it.Subject, &it.EventCode,
			&payload, &it.Attempts, &it.LastError, &it.DeadLetteredAt, &it.ReplayedAt); err != nil {
			return nil, err
		}
		it.Payload = json.RawMessage(payload)
		items = append(items, it)
	}
	return items, rows.Err()
}

// DiscardOutboxDLQ permanently removes a dead-lettered event (DLQ row first,
// then the outbox row it references).
func (r *NotificationRepository) DiscardOutboxDLQ(ctx context.Context, tenantID, outboxID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		DELETE FROM notification_outbox_dlq WHERE outbox_id = $1 AND tenant_id = $2`,
		outboxID, tenantID)
	if err != nil {
		return fmt.Errorf("delete dlq entry: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM noti_outbox WHERE id = $1`, outboxID); err != nil {
		return fmt.Errorf("delete outbox row: %w", err)
	}
	return tx.Commit()
}

// ReplayOutboxDLQForTenant requeues one dead-lettered event, scoped to the
// tenant (the admin DLQ endpoint must never cross tenant boundaries).
func (r *NotificationRepository) ReplayOutboxDLQForTenant(ctx context.Context, tenantID, outboxID, operator string) error {
	if strings.TrimSpace(operator) == "" {
		return fmt.Errorf("operator identity is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		UPDATE notification_outbox_dlq
		SET replayed_at = now(), replayed_by = $3
		WHERE outbox_id = $1 AND tenant_id = $2 AND replayed_at IS NULL`,
		outboxID, tenantID, strings.TrimSpace(operator))
	if err != nil {
		return fmt.Errorf("mark dlq replayed: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE noti_outbox
		SET status = 'pending', attempts = 0, next_retry_at = now(), locked_until = NULL, last_error = ''
		WHERE id = $1 AND status = 'dead_lettered'`, outboxID); err != nil {
		return fmt.Errorf("requeue outbox: %w", err)
	}
	return tx.Commit()
}
