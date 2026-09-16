package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/mailer"
	"github.com/arda-labs/arda/apps/notification-service/internal/netguard"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
	ardacrypto "github.com/arda-labs/arda/libs/go/arda-crypto"
)

// defaultMaxDeliveryAttempts applies when a row has no max_attempts budget.
const defaultMaxDeliveryAttempts = 6

// deliveryRepository is the repository surface the worker needs. It is an
// interface so tests can substitute a fake without a database.
type deliveryRepository interface {
	ClaimQueuedDeliveries(ctx context.Context, limit int) ([]domain.Delivery, error)
	GetDeliveryMailContext(ctx context.Context, deliveryID string) (*repository.DeliveryMailContext, error)
	ActiveSender(ctx context.Context, tenantID, channel string) (*repository.SenderConfig, string, error)
	FindTemplate(ctx context.Context, tenantID, eventCode, channel, locale string) (*repository.NotificationTemplate, error)
	RetryDelivery(ctx context.Context, id, code, message string, delay time.Duration) error
	MarkDeliveryFailed(ctx context.Context, id, code, message string) error
	MarkDeliverySent(ctx context.Context, id string) error
}

// DeliveryWorker dispatches queued deliveries. Email rides SMTP using the
// tenant sender config + optional noti_templates text; channels without a
// provider are retried up to max_attempts and then marked failed.
type DeliveryWorker struct {
	repo     deliveryRepository
	mailer   mailer.Mailer
	secret   string
	interval time.Duration
}

func NewDeliveryWorker(repo *repository.NotificationRepository, m mailer.Mailer, secret string) *DeliveryWorker {
	return newDeliveryWorker(repo, m, secret)
}

func newDeliveryWorker(repo deliveryRepository, m mailer.Mailer, secret string) *DeliveryWorker {
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
			if err := w.failDelivery(ctx, delivery, "PROVIDER_NOT_CONFIGURED", errors.New("provider dispatch is not configured yet")); err != nil {
				return err
			}
			continue
		}
		if err := w.sendEmail(ctx, delivery); err != nil {
			slog.Warn("email delivery failed",
				"delivery_id", delivery.ID,
				"attempt", delivery.AttemptCount+1,
				"max_attempts", maxAttempts(delivery),
				"err", err,
			)
			if err := w.failDelivery(ctx, delivery, "DELIVERY_FAILED", err); err != nil {
				return err
			}
			continue
		}
		if err := w.repo.MarkDeliverySent(ctx, delivery.ID); err != nil {
			return err
		}
	}
	return nil
}

// failDelivery records one failed attempt. Once the attempt budget is spent the
// delivery is marked failed instead of being deferred forever.
func (w *DeliveryWorker) failDelivery(ctx context.Context, delivery domain.Delivery, code string, cause error) error {
	message := cause.Error()
	limit := maxAttempts(delivery)
	if delivery.AttemptCount+1 >= limit {
		slog.Error("delivery attempt budget exhausted",
			"delivery_id", delivery.ID,
			"channel", delivery.Channel,
			"attempts", delivery.AttemptCount+1,
			"max_attempts", limit,
			"err", message,
		)
		return w.repo.MarkDeliveryFailed(ctx, delivery.ID, code, message)
	}
	return w.repo.RetryDelivery(ctx, delivery.ID, code, message, retryDelay(delivery.AttemptCount))
}

func maxAttempts(delivery domain.Delivery) int {
	if delivery.MaxAttempts > 0 {
		return delivery.MaxAttempts
	}
	return defaultMaxDeliveryAttempts
}

// retryDelay backs off exponentially (30s, 1m, 2m, ...) up to 15 minutes so a
// failing provider is not hammered by every tick.
func retryDelay(attempt int) time.Duration {
	const maxDelay = 15 * time.Minute
	delay := 30 * time.Second
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	return delay
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
	// Re-validate at dispatch time: sender configs stored before host
	// validation was enforced (or edited directly in the database) must not be
	// dialled.
	if err := netguard.ValidateHost(sender.Host); err != nil {
		return fmt.Errorf("email sender host is not allowed: %w", err)
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

// renderPlaceholders substitutes {{key}} with payload values. Values are used
// verbatim here; the mailer neutralises CR/LF before they reach the headers.
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
