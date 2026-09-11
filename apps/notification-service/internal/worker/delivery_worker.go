package worker

import (
	"context"
	"errors"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/mailer"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
	ardacrypto "github.com/arda-labs/arda/libs/go/arda-crypto"
)

// DeliveryWorker dispatches queued deliveries. Email rides SMTP using the
// tenant sender config + optional noti_templates text; unconfigured channels
// keep deferring (previous behaviour).
type DeliveryWorker struct {
	repo     *repository.NotificationRepository
	mailer   mailer.Mailer
	secret   string
	interval time.Duration
}

func NewDeliveryWorker(repo *repository.NotificationRepository, m mailer.Mailer, secret string) *DeliveryWorker {
	if m == nil {
		m = mailer.NewSMTP()
	}
	return &DeliveryWorker{repo: repo, mailer: m, secret: secret, interval: 2 * time.Second}
}

func (w *DeliveryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		if err := w.runOnce(ctx); err != nil {
			slog.Error("delivery worker tick failed", "err", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *DeliveryWorker) runOnce(ctx context.Context) error {
	deliveries, err := w.repo.ClaimQueuedDeliveries(ctx, 20)
	if err != nil {
		return err
	}
	for _, delivery := range deliveries {
		if delivery.Channel != domain.ChannelEmail {
			if err := w.repo.DeferDelivery(ctx, delivery.ID, "provider dispatch is not configured yet", time.Minute); err != nil {
				return err
			}
			continue
		}
		if err := w.sendEmail(ctx, delivery); err != nil {
			slog.Warn("email delivery failed", "delivery_id", delivery.ID, "err", err)
			if deferErr := w.repo.DeferDelivery(ctx, delivery.ID, err.Error(), 2*time.Minute); deferErr != nil {
				return deferErr
			}
			continue
		}
		if err := w.repo.MarkDeliverySent(ctx, delivery.ID); err != nil {
			return err
		}
	}
	return nil
}

func (w *DeliveryWorker) sendEmail(ctx context.Context, delivery domain.Delivery) error {
	mailCtx, err := w.repo.GetDeliveryMailContext(ctx, delivery.ID)
	if err != nil {
		return err
	}
	sender, encrypted, err := w.repo.ActiveSender(ctx, mailCtx.TenantID, domain.ChannelEmail)
	if err != nil {
		return err
	}
	if sender == nil {
		return errors.New("email sender config is not configured")
	}
	password := ""
	if encrypted != "" {
		decrypted, err := ardacrypto.Decrypt(encrypted, w.secret)
		if err != nil {
			return errors.New("decrypt sender password failed")
		}
		password = decrypted
	}
	to := destinationAddress(delivery.Destination)
	if to == "" {
		return errors.New("delivery destination has no email address")
	}
	subject, body, err := w.render(ctx, mailCtx)
	if err != nil {
		return err
	}
	return w.mailer.Send(ctx, mailer.Config{
		Host:        sender.Host,
		Port:        sender.Port,
		Username:    sender.Username,
		Password:    password,
		FromAddress: sender.FromAddress,
		FromName:    sender.FromName,
		UseTLS:      sender.UseTLS,
	}, mailer.Message{To: to, Subject: subject, Body: body})
}

func (w *DeliveryWorker) render(ctx context.Context, mailCtx *repository.DeliveryMailContext) (string, string, error) {
	payload := map[string]any{}
	if len(mailCtx.Payload) > 0 {
		_ = json.Unmarshal(mailCtx.Payload, &payload)
	}
	template, err := w.repo.FindTemplate(ctx, mailCtx.TenantID, mailCtx.EventType, domain.ChannelEmail, "vi-VN")
	if err != nil {
		return "", "", err
	}
	if template != nil {
		return renderPlaceholders(template.Subject, payload), renderPlaceholders(template.Body, payload), nil
	}
	subject := mailCtx.EventType
	if subject == "" {
		subject = "Arda notification"
	}
	body, _ := json.Marshal(payload)
	return subject, string(body), nil
}

func renderPlaceholders(text string, payload map[string]any) string {
	out := text
	for key, value := range payload {
		out = strings.ReplaceAll(out, "{{"+key+"}}", toString(value))
	}
	return out
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func destinationAddress(destination json.RawMessage) string {
	var parsed map[string]any
	if err := json.Unmarshal(destination, &parsed); err != nil {
		return ""
	}
	for _, key := range []string{"email", "address", "to"} {
		if value, ok := parsed[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

