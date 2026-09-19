package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

// DecisionDomainAdapter applies a recorded human decision to the owning domain
// when no BPMN service task carries it (the return/submit branches). Adapters
// must be idempotent: the dispatcher retries failures with a fixed cooldown and
// only marks a decision APPLIED after the domain confirms.
type DecisionDomainAdapter interface {
	Supports(decision repository.TaskDecision) bool
	Apply(ctx context.Context, decision repository.TaskDecision) error
}

// DecisionDispatcher moves RECORDED decisions without a worker on the BPMN
// path to APPLIED. Approve/reject decisions are confirmed by the terminal
// workers via FinishCase instead.
type DecisionDispatcher struct {
	repo     *repository.CaseRepository
	adapter  DecisionDomainAdapter
	interval time.Duration
}

func NewDecisionDispatcher(repo *repository.CaseRepository, adapter DecisionDomainAdapter) *DecisionDispatcher {
	if repo == nil || adapter == nil {
		return nil
	}
	return &DecisionDispatcher{repo: repo, adapter: adapter, interval: 15 * time.Second}
}

func (d *DecisionDispatcher) Run(ctx context.Context) {
	if d == nil {
		return
	}
	slog.Info("workflow decision dispatcher started", "interval", d.interval.String())
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("workflow decision dispatcher stopped")
			return
		case <-ticker.C:
			d.dispatchOnce(ctx)
		}
	}
}

func (d *DecisionDispatcher) dispatchOnce(ctx context.Context) {
	candidates, err := d.repo.ListDispatchCandidates(ctx, 50)
	if err != nil {
		slog.Warn("decision dispatcher: list candidates failed", "err", err)
		return
	}
	for _, decision := range candidates {
		if !d.adapter.Supports(decision) {
			continue
		}
		if err := d.adapter.Apply(ctx, decision); err != nil {
			slog.Warn("decision dispatcher: apply failed",
				"decisionId", decision.ID,
				"caseId", decision.CaseID,
				"decision", decision.Decision,
				"err", err,
			)
			if markErr := d.repo.MarkDecisionFailed(ctx, decision.ID, err.Error()); markErr != nil {
				slog.Warn("decision dispatcher: mark failed error", "decisionId", decision.ID, "err", markErr)
			}
			continue
		}
		if err := d.repo.MarkDecisionApplied(ctx, decision.ID); err != nil {
			slog.Warn("decision dispatcher: mark applied failed", "decisionId", decision.ID, "err", err)
		}
	}
}
