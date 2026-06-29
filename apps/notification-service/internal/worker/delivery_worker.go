package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
)

type DeliveryWorker struct {
	repo     *repository.NotificationRepository
	interval time.Duration
}

func NewDeliveryWorker(repo *repository.NotificationRepository) *DeliveryWorker {
	return &DeliveryWorker{repo: repo, interval: 2 * time.Second}
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
		slog.Info("notification delivery provider not configured; deferring", "delivery_id", delivery.ID, "channel", delivery.Channel)
		if err := w.repo.DeferDelivery(ctx, delivery.ID, "provider dispatch is not configured yet", time.Minute); err != nil {
			return err
		}
	}
	return nil
}
