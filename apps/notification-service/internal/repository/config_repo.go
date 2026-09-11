package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// NotificationTemplate is one configurable notification text row (X2).
type NotificationTemplate struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	EventCode string    `json:"event_code"`
	Channel   string    `json:"channel"`
	Locale    string    `json:"locale"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SenderConfig is one outbound provider config (X2).
type SenderConfig struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Channel     string    `json:"channel"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Username    string    `json:"username,omitempty"`
	PasswordEnc string    `json:"-"`
	HasPassword bool      `json:"has_password"`
	FromAddress string    `json:"from_address"`
	FromName    string    `json:"from_name,omitempty"`
	UseTLS      bool      `json:"use_tls"`
	IsActive    bool      `json:"is_active"`
	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListTemplates returns the notification template catalog.
func (r *NotificationRepository) ListTemplates(ctx context.Context, tenantID string) ([]NotificationTemplate, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, event_code, channel, locale, subject, body, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM noti_templates WHERE tenant_id = $1 ORDER BY event_code, channel, locale`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationTemplate{}
	for rows.Next() {
		var tpl NotificationTemplate
		if err := rows.Scan(&tpl.ID, &tpl.TenantID, &tpl.EventCode, &tpl.Channel, &tpl.Locale,
			&tpl.Subject, &tpl.Body, &tpl.IsActive, &tpl.CreatedBy, &tpl.CreatedAt, &tpl.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, tpl)
	}
	return out, rows.Err()
}

// UpsertTemplate creates or updates one template.
func (r *NotificationRepository) UpsertTemplate(ctx context.Context, in *NotificationTemplate) (*NotificationTemplate, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO noti_templates (tenant_id, event_code, channel, locale, subject, body, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,COALESCE($7,true),$8)
		ON CONFLICT (tenant_id, event_code, channel, locale) DO UPDATE SET
			subject = EXCLUDED.subject, body = EXCLUDED.body, is_active = EXCLUDED.is_active,
			updated_at = now(), version = noti_templates.version + 1
		RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.EventCode, in.Channel, in.Locale, in.Subject, in.Body, in.IsActive, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

// FindTemplate returns the active template for event+channel+locale.
func (r *NotificationRepository) FindTemplate(ctx context.Context, tenantID, eventCode, channel, locale string) (*NotificationTemplate, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id::text, tenant_id, event_code, channel, locale, subject, body, is_active,
		       COALESCE(created_by,''), created_at, updated_at
		FROM noti_templates
		WHERE tenant_id = $1 AND event_code = $2 AND channel = $3 AND locale = $4 AND is_active
		LIMIT 1`, tenantID, eventCode, channel, locale)
	var tpl NotificationTemplate
	err := row.Scan(&tpl.ID, &tpl.TenantID, &tpl.EventCode, &tpl.Channel, &tpl.Locale,
		&tpl.Subject, &tpl.Body, &tpl.IsActive, &tpl.CreatedBy, &tpl.CreatedAt, &tpl.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tpl, nil
}

// DeleteTemplate removes one template by id.
func (r *NotificationRepository) DeleteTemplate(ctx context.Context, tenantID, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM noti_templates WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListSenders returns sender configs (password masked).
func (r *NotificationRepository) ListSenders(ctx context.Context, tenantID string) ([]SenderConfig, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, channel, host, port, username, password_enc <> '', from_address,
		       from_name, use_tls, is_active, created_at, updated_at
		FROM noti_sender_configs WHERE tenant_id = $1 ORDER BY channel`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SenderConfig{}
	for rows.Next() {
		var cfg SenderConfig
		if err := rows.Scan(&cfg.ID, &cfg.TenantID, &cfg.Channel, &cfg.Host, &cfg.Port, &cfg.Username,
			&cfg.HasPassword, &cfg.FromAddress, &cfg.FromName, &cfg.UseTLS, &cfg.IsActive,
			&cfg.CreatedAt, &cfg.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, rows.Err()
}

// UpsertSender creates or updates the sender config for one channel.
func (r *NotificationRepository) UpsertSender(ctx context.Context, in *SenderConfig) (*SenderConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO noti_sender_configs (tenant_id, channel, host, port, username, password_enc,
			from_address, from_name, use_tls, is_active, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,COALESCE($9,true),COALESCE($10,true),$11)
		ON CONFLICT (tenant_id, channel) DO UPDATE SET host = EXCLUDED.host, port = EXCLUDED.port,
			username = EXCLUDED.username,
			password_enc = CASE WHEN EXCLUDED.password_enc = '' THEN noti_sender_configs.password_enc
				ELSE EXCLUDED.password_enc END,
			from_address = EXCLUDED.from_address, from_name = EXCLUDED.from_name,
			use_tls = EXCLUDED.use_tls, is_active = EXCLUDED.is_active,
			updated_at = now(), version = noti_sender_configs.version + 1
		RETURNING id::text, password_enc <> '', created_at, updated_at`,
		in.TenantID, in.Channel, in.Host, in.Port, in.Username, in.PasswordEnc,
		in.FromAddress, in.FromName, in.UseTLS, in.IsActive, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.HasPassword, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

// ActiveSender returns the enabled sender config (with encrypted password).
func (r *NotificationRepository) ActiveSender(ctx context.Context, tenantID, channel string) (*SenderConfig, string, error) {
	var cfg SenderConfig
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text, tenant_id, channel, host, port, username, password_enc, from_address,
		       from_name, use_tls, is_active, created_at, updated_at
		FROM noti_sender_configs WHERE tenant_id = $1 AND channel = $2 AND is_active`,
		tenantID, channel).Scan(&cfg.ID, &cfg.TenantID, &cfg.Channel, &cfg.Host, &cfg.Port,
		&cfg.Username, &cfg.PasswordEnc, &cfg.FromAddress, &cfg.FromName, &cfg.UseTLS,
		&cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return &cfg, cfg.PasswordEnc, nil
}

// DeliveryMailContext is the data needed to render + send an email delivery.
type DeliveryMailContext struct {
	TenantID   string
	EventType  string
	Payload    json.RawMessage
	Destination json.RawMessage
}

// GetDeliveryMailContext loads the notification context for one delivery.
func (r *NotificationRepository) GetDeliveryMailContext(ctx context.Context, deliveryID string) (*DeliveryMailContext, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT d.tenant_id, n.event_type, n.payload, d.destination
		FROM noti_deliveries d JOIN noti_notifications n ON n.id = d.notification_id
		WHERE d.id = $1::uuid`, deliveryID)
	var out DeliveryMailContext
	if err := row.Scan(&out.TenantID, &out.EventType, &out.Payload, &out.Destination); err != nil {
		return nil, err
	}
	return &out, nil
}
