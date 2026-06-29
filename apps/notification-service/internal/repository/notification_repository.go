package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
)

type NotificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) CreateNotificationWithDeliveries(ctx context.Context, n *domain.Notification, deliveries []domain.Delivery) (*domain.Notification, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
		INSERT INTO noti_notifications (
			public_id, tenant_id, source_service, source_event_id, event_type,
			recipients, channels, template_key, template_version, payload, status,
			idempotency_key, correlation_id, priority
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10::jsonb,$11,$12,$13,$14)
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
		RETURNING id::text, public_id, tenant_id, source_service, source_event_id, event_type,
			recipients, channels, template_key, template_version, payload, status,
			idempotency_key, correlation_id, priority, created_at, updated_at`,
		n.PublicID, n.TenantID, n.SourceService, n.SourceEventID, n.EventType,
		string(n.Recipients), string(n.Channels), n.TemplateKey, n.TemplateVersion, string(n.Payload), n.Status,
		n.IdempotencyKey, n.CorrelationID, n.Priority,
	)

	created := &domain.Notification{}
	err = scanNotification(row, created)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return r.GetNotificationByIdempotency(ctx, n.TenantID, n.IdempotencyKey)
	}
	if err != nil {
		return nil, err
	}

	for _, d := range deliveries {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO noti_deliveries (
				notification_id, tenant_id, channel, destination, provider, status, max_attempts
			) VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7)`,
			created.ID, created.TenantID, d.Channel, string(d.Destination), d.Provider, domain.DeliveryStatusQueued, d.MaxAttempts,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *NotificationRepository) GetNotificationByPublicID(ctx context.Context, publicID string) (*domain.Notification, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, public_id, tenant_id, source_service, source_event_id, event_type,
			recipients, channels, template_key, template_version, payload, status,
			idempotency_key, correlation_id, priority, created_at, updated_at
		FROM noti_notifications
		WHERE public_id = $1`, publicID)

	n := &domain.Notification{}
	if err := scanNotification(row, n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return n, nil
}

func (r *NotificationRepository) GetNotificationByIdempotency(ctx context.Context, tenantID, key string) (*domain.Notification, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, public_id, tenant_id, source_service, source_event_id, event_type,
			recipients, channels, template_key, template_version, payload, status,
			idempotency_key, correlation_id, priority, created_at, updated_at
		FROM noti_notifications
		WHERE tenant_id = $1 AND idempotency_key = $2`, tenantID, key)

	n := &domain.Notification{}
	if err := scanNotification(row, n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return n, nil
}

func (r *NotificationRepository) ClaimQueuedDeliveries(ctx context.Context, limit int) ([]domain.Delivery, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, notification_id::text, tenant_id, channel, destination, provider,
			status, attempt_count, max_attempts, next_attempt_at, last_error_code,
			last_error_message, created_at, updated_at
		FROM noti_deliveries
		WHERE status IN ('queued', 'retrying')
		  AND next_attempt_at <= now()
		  AND (locked_until IS NULL OR locked_until < now())
		ORDER BY next_attempt_at ASC, created_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	deliveries := make([]domain.Delivery, 0, limit)
	for rows.Next() {
		var d domain.Delivery
		if err := rows.Scan(&d.ID, &d.NotificationID, &d.TenantID, &d.Channel, &d.Destination,
			&d.Provider, &d.Status, &d.AttemptCount, &d.MaxAttempts, &d.NextAttemptAt,
			&d.LastErrorCode, &d.LastErrorMessage, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for _, d := range deliveries {
		if _, err := tx.ExecContext(ctx, `
			UPDATE noti_deliveries
			SET status = $2, locked_until = now() + interval '30 seconds', updated_at = now()
			WHERE id = $1`, d.ID, domain.DeliveryStatusDispatching); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return deliveries, nil
}

func (r *NotificationRepository) DeferDelivery(ctx context.Context, id, reason string, delay time.Duration) error {
	if delay <= 0 {
		delay = time.Minute
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_deliveries
		SET status = $2,
			locked_until = NULL,
			next_attempt_at = now() + ($3 * interval '1 second'),
			last_error_code = 'PROVIDER_NOT_CONFIGURED',
			last_error_message = $4,
			updated_at = now()
		WHERE id = $1`, id, domain.DeliveryStatusQueued, int(delay.Seconds()), reason)
	return err
}

func (r *NotificationRepository) MarkDeliverySent(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_deliveries
		SET status = $2, locked_until = NULL, sent_at = now(), updated_at = now()
		WHERE id = $1`, id, domain.DeliveryStatusSent)
	return err
}

func (r *NotificationRepository) MarkDeliveryFailed(ctx context.Context, id, code, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_deliveries
		SET status = $2,
			locked_until = NULL,
			attempt_count = attempt_count + 1,
			last_error_code = $3,
			last_error_message = $4,
			updated_at = now()
		WHERE id = $1`, id, domain.DeliveryStatusFailed, code, message)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanNotification(row scanner, n *domain.Notification) error {
	return row.Scan(&n.ID, &n.PublicID, &n.TenantID, &n.SourceService, &n.SourceEventID,
		&n.EventType, &n.Recipients, &n.Channels, &n.TemplateKey, &n.TemplateVersion,
		&n.Payload, &n.Status, &n.IdempotencyKey, &n.CorrelationID, &n.Priority,
		&n.CreatedAt, &n.UpdatedAt)
}
