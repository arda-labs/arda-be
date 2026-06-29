package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
)

type EventPublisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

type OutboxWorker struct {
	repo      *repository.NotificationRepository
	publisher EventPublisher
	interval  time.Duration
}

func NewOutboxWorker(repo *repository.NotificationRepository, publisher EventPublisher) *OutboxWorker {
	return &OutboxWorker{repo: repo, publisher: publisher, interval: 2 * time.Second}
}

func (w *OutboxWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		if err := w.runOnce(ctx); err != nil {
			slog.Error("notification outbox tick failed", "err", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *OutboxWorker) runOnce(ctx context.Context) error {
	events, err := w.repo.ClaimPendingOutbox(ctx, 50)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := w.publisher.Publish(ctx, event.Subject, event.Payload); err != nil {
			if markErr := w.repo.MarkOutboxFailed(ctx, event.ID, err.Error()); markErr != nil {
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
