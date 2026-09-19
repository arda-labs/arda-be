package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// ClaimSweeper returns expired claims to their candidate pool. The engine
// assignment is released best-effort: if the unassign call fails the DB row is
// already READY, and the next claimant re-assigns the engine task anyway.
type ClaimSweeper struct {
	repo     *repository.CaseRepository
	rest     *service.ZeebeRestClient
	interval time.Duration
}

func NewClaimSweeper(repo *repository.CaseRepository, rest *service.ZeebeRestClient) *ClaimSweeper {
	if repo == nil {
		return nil
	}
	return &ClaimSweeper{repo: repo, rest: rest, interval: 30 * time.Second}
}

func (s *ClaimSweeper) Run(ctx context.Context) {
	if s == nil {
		return
	}
	slog.Info("workflow claim sweeper started", "interval", s.interval.String())
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("workflow claim sweeper stopped")
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *ClaimSweeper) sweepOnce(ctx context.Context) {
	keys, err := s.repo.ReleaseExpiredClaims(ctx)
	if err != nil {
		slog.Warn("claim sweeper: release expired claims failed", "err", err)
		return
	}
	if len(keys) == 0 {
		return
	}
	for _, key := range keys {
		if s.rest == nil || !s.rest.Enabled() {
			continue
		}
		if err := s.rest.UnassignUserTask(ctx, key); err != nil {
			slog.Warn("claim sweeper: engine unassign failed", "userTaskKey", key, "err", err)
		}
	}
	slog.Info("claim sweeper released expired claims", "count", len(keys))
}
