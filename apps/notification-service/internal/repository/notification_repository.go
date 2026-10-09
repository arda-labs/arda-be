package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	ardaevents "github.com/arda-labs/arda/libs/go/arda-events"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
)

type NotificationRepository struct {
	db *sql.DB
}

var ErrStreamLeaseLimit = errors.New("notification stream limit reached")

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// ProcessEventOnce provides the transaction boundary required by an
// at-least-once consumer. A processed event is a no-op on redelivery; a new
// event runs handler inside the same transaction as the inbox marker. A
// transaction-scoped advisory lock serializes the same event while it is being
// handled. On failure the work transaction is rolled back and the failure is
// recorded separately, so a partial business mutation cannot be committed.
func (r *NotificationRepository) ProcessEventOnce(
	ctx context.Context,
	consumerName, eventID, subject, tenantID string,
	handler func(context.Context, *sql.Tx) error,
) error {
	consumerName = strings.TrimSpace(consumerName)
	eventID = strings.TrimSpace(eventID)
	subject = strings.TrimSpace(subject)
	if consumerName == "" || eventID == "" || subject == "" {
		return fmt.Errorf("consumer name, event ID and subject are required")
	}
	if handler == nil {
		return fmt.Errorf("event handler is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, consumerName+":"+eventID); err != nil {
		return fmt.Errorf("lock event: %w", err)
	}

	var claimed string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO noti_event_inbox (consumer_name, event_id, subject, tenant_id)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (consumer_name, event_id) DO UPDATE
		SET attempts = noti_event_inbox.attempts + 1
		WHERE noti_event_inbox.processed_at IS NULL
		RETURNING event_id`, consumerName, eventID, subject, strings.TrimSpace(tenantID)).Scan(&claimed)
	if errors.Is(err, sql.ErrNoRows) {
		// A processed event is deliberately idempotent. The conflicting row is
		// left untouched; this transaction only releases its lock.
		return tx.Commit()
	}
	if err != nil {
		return err
	}

	if err := handler(ctx, tx); err != nil {
		_ = tx.Rollback()
		if recordErr := r.recordEventFailure(ctx, consumerName, eventID, subject, tenantID, err.Error()); recordErr != nil {
			return fmt.Errorf("event handler failed: %v; record failure: %w", err, recordErr)
		}
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE noti_event_inbox
		SET processed_at = now(), last_error = ''
		WHERE consumer_name = $1 AND event_id = $2`, consumerName, eventID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *NotificationRepository) recordEventFailure(
	ctx context.Context, consumerName, eventID, subject, tenantID, reason string,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, consumerName+":"+eventID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE noti_event_inbox
		SET attempts = attempts + 1, last_error = $3
		WHERE consumer_name = $1 AND event_id = $2 AND processed_at IS NULL`, consumerName, eventID, reason)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO noti_event_inbox (consumer_name, event_id, subject, tenant_id, last_error)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (consumer_name, event_id) DO NOTHING`,
			consumerName, eventID, subject, strings.TrimSpace(tenantID), reason); err != nil {
			return err
		}
	}
	return tx.Commit()
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
				notification_id, tenant_id, channel, destination, provider, status, max_attempts, next_attempt_at
			) VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7,COALESCE($8,now()))`,
			created.ID, created.TenantID, d.Channel, string(d.Destination), d.Provider, domain.DeliveryStatusQueued, d.MaxAttempts, d.ScheduleAt,
		); err != nil {
			return nil, err
		}
	}

	for _, item := range inboxItems {
		if item.DedupeKey == "" {
			item.DedupeKey = item.PublicID
		}
		var eventSeq int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO noti_user_seq (tenant_id, user_id, last_event_seq) VALUES ($1,$2,1)
			ON CONFLICT (tenant_id, user_id) DO UPDATE SET last_event_seq = noti_user_seq.last_event_seq + 1
			RETURNING last_event_seq`, created.TenantID, item.UserID).Scan(&eventSeq); err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO noti_inbox (
				public_id, notification_id, tenant_id, user_id, type, title_key, body_key, params, href,
				entity_type, entity_id, dedupe_key, locale, priority, event_seq
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (tenant_id, user_id, dedupe_key) DO NOTHING`,
			item.PublicID, created.ID, created.TenantID, item.UserID, item.Type, item.TitleKey, item.BodyKey, string(item.Params), item.Href,
			item.EntityType, item.EntityID, item.DedupeKey, item.Locale, item.Priority, eventSeq,
		)
		if err != nil {
			return nil, err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if inserted == 0 {
			continue
		}
		if item.EntityType != "" && item.EntityID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE noti_inbox i SET resolved_at=r.resolved_at, resolved_reason=r.resolved_reason
				FROM noti_entity_resolutions r WHERE i.tenant_id=$1 AND i.user_id=$2 AND i.dedupe_key=$3
				AND r.tenant_id=i.tenant_id AND r.entity_type=i.entity_type AND r.entity_id=i.entity_id`,
				created.TenantID, item.UserID, item.DedupeKey); err != nil {
				return nil, err
			}
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
			"entity_type":     item.EntityType,
			"entity_id":       item.EntityID,
			"event_seq":       eventSeq,
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const maxAttempts = 10
	var attempts int
	var tenantID, subject string
	var payload []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT tenant_id, subject, payload, attempts
		FROM noti_outbox WHERE id = $1 FOR UPDATE`, id).Scan(&tenantID, &subject, &payload, &attempts); err != nil {
		return err
	}
	if attempts >= maxAttempts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO notification_outbox_dlq (outbox_id, tenant_id, subject, payload, attempts, last_error)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6)
			ON CONFLICT (outbox_id) DO UPDATE SET attempts = EXCLUDED.attempts,
			last_error = EXCLUDED.last_error, dead_lettered_at = now(), replayed_at = NULL, replayed_by = NULL`,
			id, tenantID, subject, string(payload), attempts, reason); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE noti_outbox SET status = 'dead_lettered', locked_until = NULL, last_error = $2 WHERE id = $1`, id, reason); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `
		UPDATE noti_outbox
		SET status = 'pending',
			next_retry_at = now() + interval '30 seconds' * LEAST($3, 120),
			locked_until = NULL,
			last_error = $2
		WHERE id = $1`, id, reason, attempts); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplayOutboxDLQ requeues one dead-lettered event only when an operator
// identity is supplied. It is intentionally bounded to one event per call so
// a replay cannot flood the stream or accidentally cross tenant boundaries.
func (r *NotificationRepository) ReplayOutboxDLQ(ctx context.Context, id, operator string) error {
	if strings.TrimSpace(operator) == "" {
		return fmt.Errorf("operator identity is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE notification_outbox_dlq
		SET replayed_at = now(), replayed_by = $2
		WHERE outbox_id = $1 AND replayed_at IS NULL`, id, strings.TrimSpace(operator))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE noti_outbox
		SET status = 'pending', attempts = 0, next_retry_at = now(), locked_until = NULL, last_error = ''
		WHERE id = $1 AND status = 'dead_lettered'`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *NotificationRepository) ListInbox(ctx context.Context, tenantID, userID string, limit int) ([]domain.InboxItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, public_id, tenant_id, user_id, type, title_key, body_key, params, href, read_at,
			entity_type, entity_id, dedupe_key, resolved_at, resolved_reason, superseded_at, expires_at, locale, priority, event_seq, created_at
		FROM noti_inbox
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY event_seq DESC
		LIMIT $3`, tenantID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.InboxItem, 0, limit)
	for rows.Next() {
		var item domain.InboxItem
		if err := rows.Scan(&item.ID, &item.PublicID, &item.TenantID, &item.UserID, &item.Type,
			&item.TitleKey, &item.BodyKey, &item.Params, &item.Href, &item.ReadAt,
			&item.EntityType, &item.EntityID, &item.DedupeKey, &item.ResolvedAt, &item.ResolvedReason, &item.SupersededAt, &item.ExpiresAt, &item.Locale, &item.Priority, &item.EventSeq, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *NotificationRepository) ListInboxAfter(ctx context.Context, tenantID, userID string, afterSeq int64, limit int) ([]domain.InboxItem, error) {
	if limit < 1 || limit > 101 {
		limit = 101
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, public_id, tenant_id, user_id, type, title_key, body_key, params, href, read_at,
		       entity_type, entity_id, dedupe_key, resolved_at, resolved_reason, superseded_at, expires_at,
	       locale, priority, event_seq, created_at
		FROM noti_inbox
		WHERE tenant_id = $1 AND user_id = $2 AND event_seq > $3
		ORDER BY event_seq ASC LIMIT $4`, tenantID, userID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.InboxItem, 0, limit)
	for rows.Next() {
		var item domain.InboxItem
		if err := rows.Scan(&item.ID, &item.PublicID, &item.TenantID, &item.UserID, &item.Type,
			&item.TitleKey, &item.BodyKey, &item.Params, &item.Href, &item.ReadAt,
			&item.EntityType, &item.EntityID, &item.DedupeKey, &item.ResolvedAt, &item.ResolvedReason,
			&item.SupersededAt, &item.ExpiresAt, &item.Locale, &item.Priority, &item.EventSeq, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *NotificationRepository) LatestInboxEventSeq(ctx context.Context, tenantID, userID string) (int64, error) {
	var seq int64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(last_event_seq, 0) FROM noti_user_seq WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return seq, err
}

func (r *NotificationRepository) ListEntityUserIDs(ctx context.Context, tenantID, entityType, entityID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT user_id FROM noti_inbox WHERE tenant_id = $1 AND entity_type = $2 AND entity_id = $3 ORDER BY user_id`, tenantID, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]string, 0)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		users = append(users, userID)
	}
	return users, rows.Err()
}

func (r *NotificationRepository) AcquireStreamLease(ctx context.Context, tenantID, userID string, limit int, ttl time.Duration) (string, error) {
	var tokenBytes [16]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return "", err
	}
	token := fmt.Sprintf("%x", tokenBytes[:])
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "notification-stream:"+tenantID+":"+userID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM noti_stream_lease WHERE tenant_id=$1 AND user_id=$2 AND expires_at <= now()`, tenantID, userID); err != nil {
		return "", err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM noti_stream_lease WHERE tenant_id=$1 AND user_id=$2`, tenantID, userID).Scan(&count); err != nil {
		return "", err
	}
	if count >= limit {
		return "", ErrStreamLeaseLimit
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO noti_stream_lease (lease_id,tenant_id,user_id,expires_at) VALUES ($1,$2,$3,now()+($4 * interval '1 second'))`, token, tenantID, userID, ttl.Seconds()); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (r *NotificationRepository) RenewStreamLease(ctx context.Context, tenantID, userID, token string, ttl time.Duration) error {
	result, err := r.db.ExecContext(ctx, `UPDATE noti_stream_lease SET expires_at=now()+($4 * interval '1 second') WHERE lease_id=$1 AND tenant_id=$2 AND user_id=$3 AND expires_at > now()`, token, tenantID, userID, ttl.Seconds())
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return errors.New("notification stream lease expired")
	}
	return nil
}

func (r *NotificationRepository) ReleaseStreamLease(ctx context.Context, tenantID, userID, token string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM noti_stream_lease WHERE lease_id=$1 AND tenant_id=$2 AND user_id=$3`, token, tenantID, userID)
	return err
}

func (r *NotificationRepository) ListPreferences(ctx context.Context, tenantID, userID string) ([]domain.NotificationPreference, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT event_group, channel, enabled,
		CASE WHEN quiet_start IS NULL THEN NULL ELSE to_char(quiet_start, 'HH24:MI') END,
		CASE WHEN quiet_end IS NULL THEN NULL ELSE to_char(quiet_end, 'HH24:MI') END,
		tz, digest_mode, locale FROM noti_preferences WHERE tenant_id=$1 AND user_id=$2 ORDER BY event_group, channel`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.NotificationPreference{}
	for rows.Next() {
		var p domain.NotificationPreference
		if err := rows.Scan(&p.EventGroup, &p.Channel, &p.Enabled, &p.QuietStart, &p.QuietEnd, &p.Timezone, &p.DigestMode, &p.Locale); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (r *NotificationRepository) SavePreference(ctx context.Context, tenantID, userID string, p domain.NotificationPreference) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO noti_preferences (tenant_id,user_id,event_group,channel,enabled,quiet_start,quiet_end,tz,digest_mode,locale)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::time,NULLIF($7,'')::time,$8,$9,$10)
		ON CONFLICT (tenant_id,user_id,event_group,channel) DO UPDATE SET enabled=EXCLUDED.enabled, quiet_start=EXCLUDED.quiet_start, quiet_end=EXCLUDED.quiet_end, tz=EXCLUDED.tz, digest_mode=EXCLUDED.digest_mode, locale=EXCLUDED.locale, updated_at=now()`,
		tenantID, userID, strings.TrimSpace(p.EventGroup), strings.TrimSpace(p.Channel), p.Enabled, nullableString(p.QuietStart), nullableString(p.QuietEnd), defaultValue(p.Timezone, "UTC"), defaultValue(strings.ToUpper(p.DigestMode), "NONE"), strings.TrimSpace(p.Locale))
	return err
}

// DeliveryRule honors a channel preference and computes the next quiet-hours
// boundary from database time so queued delivery does not rely on host clocks.
func (r *NotificationRepository) DeliveryRule(ctx context.Context, tenantID, userID, eventGroup, channel string) (bool, *time.Time, error) {
	var enabled bool
	var scheduled sql.NullTime
	err := r.db.QueryRowContext(ctx, `WITH preference AS (
		SELECT enabled, quiet_start, quiet_end, tz FROM noti_preferences
		WHERE tenant_id=$1 AND user_id=$2 AND channel=$4 AND event_group IN ($3,'*')
		ORDER BY CASE WHEN event_group=$3 THEN 0 ELSE 1 END LIMIT 1
	)
	SELECT COALESCE(p.enabled,TRUE), CASE WHEN p.quiet_start IS NULL OR $4='in_app' THEN NULL ELSE
		CASE WHEN p.quiet_start=p.quiet_end
			OR (p.quiet_start<p.quiet_end AND (now() AT TIME ZONE p.tz)::time >= p.quiet_start AND (now() AT TIME ZONE p.tz)::time < p.quiet_end)
			OR (p.quiet_start>p.quiet_end AND ((now() AT TIME ZONE p.tz)::time >= p.quiet_start OR (now() AT TIME ZONE p.tz)::time < p.quiet_end))
		THEN (((now() AT TIME ZONE p.tz)::date + CASE WHEN p.quiet_start>=p.quiet_end AND (now() AT TIME ZONE p.tz)::time>=p.quiet_start THEN 1 ELSE 0 END) + p.quiet_end) AT TIME ZONE p.tz
		ELSE NULL END END
	FROM (SELECT 1) seed LEFT JOIN preference p ON TRUE`, tenantID, userID, eventGroup, channel).Scan(&enabled, &scheduled)
	if err != nil {
		return false, nil, err
	}
	if !scheduled.Valid {
		return enabled, nil, nil
	}
	return enabled, &scheduled.Time, nil
}

func (r *NotificationRepository) PreferredLocale(ctx context.Context, tenantID, userID string) (string, error) {
	var locale string
	err := r.db.QueryRowContext(ctx, `SELECT locale FROM noti_preferences WHERE tenant_id=$1 AND user_id=$2 AND locale<>'' ORDER BY updated_at DESC LIMIT 1`, tenantID, userID).Scan(&locale)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return locale, err
}

func nullableString(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
func defaultValue(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// ResolveEntity persists a tombstone before resolving existing inbox rows, closing the
// race where a completion arrives before its corresponding assignment.
func (r *NotificationRepository) ResolveEntity(ctx context.Context, tx *sql.Tx, tenantID, entityType, entityID, reason, eventID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO noti_entity_resolutions (tenant_id,entity_type,entity_id,resolved_reason,source_event_id)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (tenant_id,entity_type,entity_id) DO UPDATE SET resolved_at=now(),resolved_reason=EXCLUDED.resolved_reason,source_event_id=EXCLUDED.source_event_id`, tenantID, entityType, entityID, reason, eventID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE noti_inbox SET resolved_at=COALESCE(resolved_at,now()), resolved_reason=$4 WHERE tenant_id=$1 AND entity_type=$2 AND entity_id=$3 AND resolved_at IS NULL`, tenantID, entityType, entityID, reason)
	return err
}

func (r *NotificationRepository) RecordRecipientWarning(ctx context.Context, tenantID, eventID, entityType, entityID, reason string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO noti_recipient_warnings (tenant_id,event_id,entity_type,entity_id,reason)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (tenant_id,event_id) DO UPDATE SET reason=EXCLUDED.reason,created_at=now()`,
		strings.TrimSpace(tenantID), strings.TrimSpace(eventID), strings.TrimSpace(entityType), strings.TrimSpace(entityID), strings.TrimSpace(reason))
	return err
}

func (r *NotificationRepository) UnreadCount(ctx context.Context, tenantID, userID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM noti_inbox
		WHERE tenant_id = $1 AND user_id = $2 AND read_at IS NULL AND resolved_at IS NULL AND superseded_at IS NULL`, tenantID, userID).Scan(&count)
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

func (r *NotificationRepository) GetNotificationByPublicID(ctx context.Context, tenantID, publicID string) (*domain.Notification, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, public_id, tenant_id, source_service, source_event_id, event_type,
			recipients, channels, template_key, template_version, payload, status,
			idempotency_key, correlation_id, priority, created_at, updated_at
		FROM noti_notifications
		WHERE tenant_id = $1 AND public_id = $2`, tenantID, publicID)

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

// RetryDelivery records one failed attempt and requeues the delivery after
// delay. attempt_count is incremented here so callers can enforce
// max_attempts; a delivery that has spent its budget must be terminated with
// MarkDeliveryFailed instead of being deferred forever.
func (r *NotificationRepository) RetryDelivery(ctx context.Context, id, code, message string, delay time.Duration) error {
	if delay <= 0 {
		delay = time.Minute
	}
	if strings.TrimSpace(code) == "" {
		code = "DELIVERY_FAILED"
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_deliveries
		SET status = $2,
			attempt_count = attempt_count + 1,
			locked_until = NULL,
			next_attempt_at = now() + ($3 * interval '1 second'),
			last_error_code = $4,
			last_error_message = $5,
			updated_at = now()
		WHERE id = $1`, id, domain.DeliveryStatusQueued, int(delay.Seconds()), code, message)
	return err
}

// DeferDelivery keeps the legacy signature for channels without a provider. It
// counts the attempt like RetryDelivery so unsupported channels stop being
// requeued once max_attempts is reached.
func (r *NotificationRepository) DeferDelivery(ctx context.Context, id, reason string, delay time.Duration) error {
	return r.RetryDelivery(ctx, id, "PROVIDER_NOT_CONFIGURED", reason, delay)
}

func (r *NotificationRepository) MarkDeliverySent(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE noti_deliveries
		SET status = $2, locked_until = NULL, sent_at = now(), updated_at = now()
		WHERE id = $1`, id, domain.DeliveryStatusSent)
	return err
}

// MarkDeliveryFailed terminates a delivery whose attempt budget is spent.
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

// ErrPushSubscriptionOwnedByAnotherUser is returned when an upsert targets an
// endpoint that already belongs to a different tenant or user. The endpoint is
// a capability URL: rebinding it to another account would leak that account's
// notifications, so the existing owner always wins.
var ErrPushSubscriptionOwnedByAnotherUser = errors.New("push endpoint is already registered to another user")

func (r *NotificationRepository) UpsertPushSubscription(ctx context.Context, item PushSubscription) error {
	// The ON CONFLICT ... WHERE guard keeps the original owner: the update only
	// runs when the stored row has the same tenant_id and user_id. When another
	// account owns the endpoint the statement updates nothing and RETURNING
	// yields no row, which is reported as ErrPushSubscriptionOwnedByAnotherUser.
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO noti_push_subscriptions (
			tenant_id, user_id, endpoint, p256dh, auth, user_agent
		) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (endpoint) DO UPDATE SET
			p256dh = EXCLUDED.p256dh,
			auth = EXCLUDED.auth,
			user_agent = EXCLUDED.user_agent,
			updated_at = now()
		WHERE noti_push_subscriptions.tenant_id = EXCLUDED.tenant_id
		  AND noti_push_subscriptions.user_id = EXCLUDED.user_id
		RETURNING id::text`,
		item.TenantID, item.UserID, item.Endpoint, item.P256dh, item.Auth, item.UserAgent,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPushSubscriptionOwnedByAnotherUser
	}
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

// DeletePushSubscriptionByEndpoint removes one endpoint for its owner. It is
// scoped by user_id as well as tenant_id so a caller can never remove another
// account's subscription.
func (r *NotificationRepository) DeletePushSubscriptionByEndpoint(ctx context.Context, tenantID, userID, endpoint string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM noti_push_subscriptions
		WHERE tenant_id = $1 AND user_id = $2 AND endpoint = $3`, tenantID, userID, endpoint)
	return err
}

func scanNotification(row scanner, n *domain.Notification) error {
	return row.Scan(&n.ID, &n.PublicID, &n.TenantID, &n.SourceService, &n.SourceEventID,
		&n.EventType, &n.Recipients, &n.Channels, &n.TemplateKey, &n.TemplateVersion,
		&n.Payload, &n.Status, &n.IdempotencyKey, &n.CorrelationID, &n.Priority,
		&n.CreatedAt, &n.UpdatedAt)
}

func insertOutbox(ctx context.Context, tx *sql.Tx, subject, eventCode, aggregateType, aggregateID, tenantID, userID string, payload any) error {
	meta := ardametadata.FromOutgoing(ctx)
	env, err := ardaevents.NewEnvelope(eventCode, payload, ardaevents.Options{
		SourceService: "notification-service",
		TenantID:      tenantID,
		OrgID:         meta.OrgID,
		RequestID:     meta.RequestID,
		TraceID:       meta.TraceID,
		TraceParent:   meta.TraceParent,
		Locale:        meta.Locale,
		Actor: ardaevents.Actor{
			UserID:         meta.ActorUserID,
			UserSubject:    meta.UserSubject,
			ServiceAccount: meta.ServiceAccount,
		},
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
