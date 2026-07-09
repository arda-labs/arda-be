package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	ardaevents "github.com/arda-labs/arda/libs/go/arda-events"
)

type NotificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) CreateNotification(ctx context.Context, n *domain.Notification, deliveries []domain.Delivery, inboxItems []domain.InboxItem) (*domain.Notification, error) {
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

	for _, item := range inboxItems {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO noti_inbox (
				public_id, notification_id, tenant_id, user_id, type, title_key, body_key, params, href
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9)`,
			item.PublicID, created.ID, created.TenantID, item.UserID, item.Type, item.TitleKey, item.BodyKey, string(item.Params), item.Href,
		); err != nil {
			return nil, err
		}
		if err := insertOutbox(ctx, tx, ardaevents.SubjectNotificationInboxCreated, ardaevents.EventNotificationInboxCreated, "noti_inbox", item.PublicID, created.TenantID, item.UserID, map[string]any{
			"notification_id": created.PublicID,
			"inbox_id":        item.PublicID,
			"tenant_id":       created.TenantID,
			"user_id":         item.UserID,
			"type":            item.Type,
			"title_key":       item.TitleKey,
			"body_key":        item.BodyKey,
			"params":          json.RawMessage(item.Params),
			"href":            item.Href,
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *NotificationRepository) ClaimPendingOutbox(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id::text, subject, payload, attempts, created_at
		FROM noti_outbox
		WHERE status IN ('pending', 'publishing')
		  AND next_retry_at <= now()
		  AND (locked_until IS NULL OR locked_until < now())
		ORDER BY created_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]domain.OutboxEvent, 0, limit)
	for rows.Next() {
		var event domain.OutboxEvent
		if err := rows.Scan(&event.ID, &event.Subject, &event.Payload, &event.Attempts, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for _, event := range events {
		if _, err := tx.ExecContext(ctx, `
			UPDATE noti_outbox
			SET status = 'publishing',
				attempts = attempts + 1,
				locked_until = now() + interval '30 seconds'
			WHERE id = $1`, event.ID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *NotificationRepository) MarkOutboxPublished(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_outbox
		SET status = 'published', published_at = now(), locked_until = NULL, last_error = ''
		WHERE id = $1`, id)
	return err
}

func (r *NotificationRepository) MarkOutboxFailed(ctx context.Context, id, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_outbox
		SET status = 'pending',
			next_retry_at = now() + interval '30 seconds',
			locked_until = NULL,
			last_error = $2
		WHERE id = $1`, id, reason)
	return err
}

func (r *NotificationRepository) ListInbox(ctx context.Context, tenantID, userID string, limit int) ([]domain.InboxItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, public_id, tenant_id, user_id, type, title_key, body_key, params, href, read_at, created_at
		FROM noti_inbox
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY created_at DESC
		LIMIT $3`, tenantID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.InboxItem, 0, limit)
	for rows.Next() {
		var item domain.InboxItem
		if err := rows.Scan(&item.ID, &item.PublicID, &item.TenantID, &item.UserID, &item.Type,
			&item.TitleKey, &item.BodyKey, &item.Params, &item.Href, &item.ReadAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *NotificationRepository) UnreadCount(ctx context.Context, tenantID, userID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM noti_inbox
		WHERE tenant_id = $1 AND user_id = $2 AND read_at IS NULL`, tenantID, userID).Scan(&count)
	return count, err
}

func (r *NotificationRepository) MarkInboxRead(ctx context.Context, tenantID, userID, publicID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_inbox
		SET read_at = COALESCE(read_at, now())
		WHERE tenant_id = $1 AND user_id = $2 AND public_id = $3`, tenantID, userID, publicID)
	return err
}

func (r *NotificationRepository) MarkAllInboxRead(ctx context.Context, tenantID, userID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_inbox
		SET read_at = COALESCE(read_at, now())
		WHERE tenant_id = $1 AND user_id = $2 AND read_at IS NULL`, tenantID, userID)
	return err
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

type PushSubscription struct {
	ID        string
	TenantID  string
	UserID    string
	Endpoint  string
	P256dh    string
	Auth      string
	UserAgent string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (r *NotificationRepository) UpsertPushSubscription(ctx context.Context, item PushSubscription) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO noti_push_subscriptions (
			tenant_id, user_id, endpoint, p256dh, auth, user_agent
		) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (endpoint) DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			user_id = EXCLUDED.user_id,
			p256dh = EXCLUDED.p256dh,
			auth = EXCLUDED.auth,
			user_agent = EXCLUDED.user_agent,
			updated_at = now()`,
		item.TenantID, item.UserID, item.Endpoint, item.P256dh, item.Auth, item.UserAgent,
	)
	return err
}

func (r *NotificationRepository) DeletePushSubscription(ctx context.Context, tenantID, userID, endpoint string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM noti_push_subscriptions
		WHERE tenant_id = $1 AND user_id = $2 AND endpoint = $3`,
		tenantID, userID, endpoint,
	)
	return err
}

func (r *NotificationRepository) ListPushSubscriptions(ctx context.Context, tenantID, userID string) ([]PushSubscription, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, user_id, endpoint, p256dh, auth, user_agent, created_at, updated_at
		FROM noti_push_subscriptions
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY updated_at DESC`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PushSubscription, 0)
	for rows.Next() {
		var item PushSubscription
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.UserID, &item.Endpoint, &item.P256dh, &item.Auth,
			&item.UserAgent, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *NotificationRepository) DeletePushSubscriptionByEndpoint(ctx context.Context, endpoint string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM noti_push_subscriptions WHERE endpoint = $1`, endpoint)
	return err
}

func scanNotification(row scanner, n *domain.Notification) error {
	return row.Scan(&n.ID, &n.PublicID, &n.TenantID, &n.SourceService, &n.SourceEventID,
		&n.EventType, &n.Recipients, &n.Channels, &n.TemplateKey, &n.TemplateVersion,
		&n.Payload, &n.Status, &n.IdempotencyKey, &n.CorrelationID, &n.Priority,
		&n.CreatedAt, &n.UpdatedAt)
}

func insertOutbox(ctx context.Context, tx *sql.Tx, subject, eventCode, aggregateType, aggregateID, tenantID, userID string, payload any) error {
	env, err := ardaevents.NewEnvelope(eventCode, payload, ardaevents.Options{
		SourceService: "notification-service",
		TenantID:      tenantID,
	})
	if err != nil {
		return err
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO noti_outbox (
			subject, event_code, aggregate_type, aggregate_id, tenant_id, user_id, payload
		) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)`,
		subject, eventCode, aggregateType, aggregateID, tenantID, userID, string(data))
	return err
}
