package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/media-service/internal/events"
	"github.com/arda-labs/arda/apps/media-service/internal/repository"
)

// EventPublisher publishes one claimed outbox payload to the event bus.
type EventPublisher interface {
	Publish(ctx context.Context, subject, eventID string, payload []byte) error
}

// OutboxWorker drains media_outbox_events to NATS JetStream. Failed rows stay
// pending (with backoff) so a broker outage never loses upload events.
type OutboxWorker struct {
	repo      *repository.MediaRepository
	publisher EventPublisher
	interval  time.Duration
	batchSize int
}

func NewOutboxWorker(repo *repository.MediaRepository, publisher EventPublisher) *OutboxWorker {
	return &OutboxWorker{repo: repo, publisher: publisher, interval: 2 * time.Second, batchSize: 50}
}

func (w *OutboxWorker) Run(ctx context.Context) {
	if w == nil || w.publisher == nil || w.repo == nil {
		return
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	slog.Info("media outbox relay started", "interval", w.interval.String())
	for {
		if err := w.runOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("media outbox publish tick failed", "err", err)
		}
		select {
		case <-ctx.Done():
			slog.Info("media outbox relay stopped")
			return
		case <-ticker.C:
		}
	}
}

func (w *OutboxWorker) runOnce(ctx context.Context) error {
	batch, err := w.repo.ClaimPendingOutbox(ctx, w.batchSize)
	if err != nil {
		return err
	}
	for _, event := range batch {
		subject := events.SubjectForEventType(event.EventType)
		if err := w.publisher.Publish(ctx, subject, event.ID, event.Payload); err != nil {
			slog.Error("media outbox publish failed", "event_id", event.ID, "subject", subject, "attempts", event.Attempts, "err", err)
			if markErr := w.repo.MarkOutboxFailed(ctx, event.ID); markErr != nil {
				return markErr
			}
			continue
		}
		if err := w.repo.MarkOutboxPublished(ctx, event.ID); err != nil {
			return err
		}
	}
	return nil
}
