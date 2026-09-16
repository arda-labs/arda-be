package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/domain"
	"github.com/arda-labs/arda/apps/notification-service/internal/mailer"
	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
)

type fakeMailer struct {
	err error
}

func (f fakeMailer) Send(context.Context, mailer.Config, mailer.Message) error {
	return f.err
}

type retryCall struct {
	id    string
	code  string
	delay time.Duration
}

type failureCall struct {
	id   string
	code string
}

type fakeDeliveryRepo struct {
	deliveries []domain.Delivery
	senderHost string

	retries  []retryCall
	failures []failureCall
	sent     []string
}

func (f *fakeDeliveryRepo) ClaimQueuedDeliveries(context.Context, int) ([]domain.Delivery, error) {
	return f.deliveries, nil
}

func (f *fakeDeliveryRepo) GetDeliveryMailContext(context.Context, string) (*repository.DeliveryMailContext, error) {
	return &repository.DeliveryMailContext{
		TenantID:    "tenant-acme",
		EventType:   "approval.requested",
		Payload:     []byte(`{}`),
		Destination: []byte(`{"email":"user@example.com"}`),
	}, nil
}

func (f *fakeDeliveryRepo) ActiveSender(context.Context, string, string) (*repository.SenderConfig, string, error) {
	host := f.senderHost
	if host == "" {
		host = "smtp.example.com"
	}
	return &repository.SenderConfig{Host: host, FromAddress: "ops@example.com"}, "", nil
}

func (f *fakeDeliveryRepo) FindTemplate(context.Context, string, string, string, string) (*repository.NotificationTemplate, error) {
	return nil, nil
}

func (f *fakeDeliveryRepo) RetryDelivery(_ context.Context, id, code, _ string, delay time.Duration) error {
	f.retries = append(f.retries, retryCall{id: id, code: code, delay: delay})
	return nil
}

func (f *fakeDeliveryRepo) MarkDeliveryFailed(_ context.Context, id, code, _ string) error {
	f.failures = append(f.failures, failureCall{id: id, code: code})
	return nil
}

func (f *fakeDeliveryRepo) MarkDeliverySent(_ context.Context, id string) error {
	f.sent = append(f.sent, id)
	return nil
}

func emailDelivery(attemptCount, maxAttempts int) domain.Delivery {
	return domain.Delivery{
		ID:           "delivery-1",
		Channel:      domain.ChannelEmail,
		Destination:  []byte(`{"email":"user@example.com"}`),
		AttemptCount: attemptCount,
		MaxAttempts:  maxAttempts,
	}
}

func TestDeliveryWorkerRetriesFirstFailure(t *testing.T) {
	repo := &fakeDeliveryRepo{deliveries: []domain.Delivery{emailDelivery(0, 3)}}
	w := newDeliveryWorker(repo, fakeMailer{err: errors.New("smtp unavailable")}, "")

	if err := w.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if len(repo.retries) != 1 {
		t.Fatalf("retries = %+v, want exactly one retry", repo.retries)
	}
	if repo.retries[0].id != "delivery-1" || repo.retries[0].code != "DELIVERY_FAILED" {
		t.Fatalf("unexpected retry call: %+v", repo.retries[0])
	}
	if repo.retries[0].delay <= 0 {
		t.Fatalf("retry delay = %v, want positive backoff", repo.retries[0].delay)
	}
	if len(repo.failures) != 0 {
		t.Fatalf("failures = %+v, want none before the attempt budget is spent", repo.failures)
	}
	if len(repo.sent) != 0 {
		t.Fatalf("sent = %v, want none", repo.sent)
	}
}

func TestDeliveryWorkerFailsAfterMaxAttempts(t *testing.T) {
	repo := &fakeDeliveryRepo{deliveries: []domain.Delivery{emailDelivery(2, 3)}}
	w := newDeliveryWorker(repo, fakeMailer{err: errors.New("smtp unavailable")}, "")

	if err := w.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if len(repo.failures) != 1 {
		t.Fatalf("failures = %+v, want the delivery marked failed", repo.failures)
	}
	if repo.failures[0].id != "delivery-1" || repo.failures[0].code != "DELIVERY_FAILED" {
		t.Fatalf("unexpected failure call: %+v", repo.failures[0])
	}
	if len(repo.retries) != 0 {
		t.Fatalf("retries = %+v, want none once max_attempts is reached", repo.retries)
	}
}

func TestDeliveryWorkerCapsUnsupportedChannels(t *testing.T) {
	t.Run("under budget is retried", func(t *testing.T) {
		repo := &fakeDeliveryRepo{deliveries: []domain.Delivery{{
			ID: "delivery-push", Channel: domain.ChannelPush, AttemptCount: 0, MaxAttempts: 2,
		}}}
		w := newDeliveryWorker(repo, fakeMailer{}, "")
		if err := w.runOnce(context.Background()); err != nil {
			t.Fatalf("runOnce: %v", err)
		}
		if len(repo.retries) != 1 || repo.retries[0].code != "PROVIDER_NOT_CONFIGURED" {
			t.Fatalf("retries = %+v, want one PROVIDER_NOT_CONFIGURED retry", repo.retries)
		}
		if len(repo.failures) != 0 {
			t.Fatalf("failures = %+v, want none yet", repo.failures)
		}
	})

	t.Run("budget spent is failed", func(t *testing.T) {
		repo := &fakeDeliveryRepo{deliveries: []domain.Delivery{{
			ID: "delivery-sms", Channel: domain.ChannelSMS, AttemptCount: 1, MaxAttempts: 2,
		}}}
		w := newDeliveryWorker(repo, fakeMailer{}, "")
		if err := w.runOnce(context.Background()); err != nil {
			t.Fatalf("runOnce: %v", err)
		}
		if len(repo.failures) != 1 || repo.failures[0].code != "PROVIDER_NOT_CONFIGURED" {
			t.Fatalf("failures = %+v, want one PROVIDER_NOT_CONFIGURED failure", repo.failures)
		}
		if len(repo.retries) != 0 {
			t.Fatalf("retries = %+v, want none", repo.retries)
		}
	})
}

func TestDeliveryWorkerMarksSentOnSuccess(t *testing.T) {
	repo := &fakeDeliveryRepo{deliveries: []domain.Delivery{emailDelivery(0, 3)}}
	w := newDeliveryWorker(repo, fakeMailer{}, "")

	if err := w.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if len(repo.sent) != 1 || repo.sent[0] != "delivery-1" {
		t.Fatalf("sent = %v, want [delivery-1]", repo.sent)
	}
	if len(repo.retries) != 0 || len(repo.failures) != 0 {
		t.Fatalf("retries = %+v failures = %+v, want none", repo.retries, repo.failures)
	}
}

func TestDeliveryWorkerFallsBackToDefaultMaxAttempts(t *testing.T) {
	repo := &fakeDeliveryRepo{deliveries: []domain.Delivery{emailDelivery(defaultMaxDeliveryAttempts-1, 0)}}
	w := newDeliveryWorker(repo, fakeMailer{err: errors.New("smtp unavailable")}, "")

	if err := w.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	if len(repo.failures) != 1 {
		t.Fatalf("failures = %+v, want the default budget to terminate the delivery", repo.failures)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	if got := retryDelay(0); got != 30*time.Second {
		t.Fatalf("retryDelay(0) = %v, want 30s", got)
	}
	if got := retryDelay(3); got != 4*time.Minute {
		t.Fatalf("retryDelay(3) = %v, want 4m", got)
	}
	if got := retryDelay(50); got != 15*time.Minute {
		t.Fatalf("retryDelay(50) = %v, want the 15m cap", got)
	}
}

type recordingMailer struct{ sent int }

func (m *recordingMailer) Send(context.Context, mailer.Config, mailer.Message) error {
	m.sent++
	return nil
}

// TestDeliveryWorkerRejectsUnsafeSenderHost proves the dispatch-time guard:
// even when a legacy sender row points at an internal address, no SMTP
// connection is attempted.
func TestDeliveryWorkerRejectsUnsafeSenderHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.0.0.7", "169.254.169.254", "smtp.internal", "100.64.0.9", "localhost"} {
		t.Run(host, func(t *testing.T) {
			repo := &fakeDeliveryRepo{
				deliveries: []domain.Delivery{emailDelivery(0, 3)},
				senderHost: host,
			}
			m := &recordingMailer{}
			w := newDeliveryWorker(repo, m, "")

			if err := w.runOnce(context.Background()); err != nil {
				t.Fatalf("runOnce: %v", err)
			}
			if m.sent != 0 {
				t.Fatalf("mailer was called %d times for blocked host %q", m.sent, host)
			}
			if len(repo.retries) != 1 || repo.retries[0].code != "DELIVERY_FAILED" {
				t.Fatalf("retries = %+v, want one DELIVERY_FAILED retry", repo.retries)
			}
		})
	}
}
