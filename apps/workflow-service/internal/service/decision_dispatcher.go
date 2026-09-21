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
	adapters []DecisionDomainAdapter
	interval time.Duration
	warned   map[string]struct{}
}

// NewDecisionDispatcher accepts one adapter per domain (CRM, loan, deposit, …).
// A decision is applied by the first adapter that Supports it; domains without
// an adapter yet keep their row RECORDED and are warned about after a grace
// period instead of being retried silently.
func NewDecisionDispatcher(repo *repository.CaseRepository, adapters ...DecisionDomainAdapter) *DecisionDispatcher {
	if repo == nil {
		return nil
	}
	kept := make([]DecisionDomainAdapter, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter != nil {
			kept = append(kept, adapter)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return &DecisionDispatcher{
		repo:     repo,
		adapters: kept,
		interval: 15 * time.Second,
		warned:   make(map[string]struct{}),
	}
}

// adapterFor returns the first adapter that supports the decision, or nil when
// no domain owns it yet.
func (d *DecisionDispatcher) adapterFor(decision repository.TaskDecision) DecisionDomainAdapter {
	for _, adapter := range d.adapters {
		if adapter.Supports(decision) {
			return adapter
		}
	}
	return nil
}

// unroutedGracePeriod gives a decision time to be picked up before warning:
// the row is only recorded when a human completes a return/submit branch.
const unroutedGracePeriod = 5 * time.Minute

// warnUnrouted logs once per (case type, decision) when no adapter owns a
// decision that has been waiting longer than the grace period.
func (d *DecisionDispatcher) warnUnrouted(decision repository.TaskDecision) {
	if time.Since(decision.RecordedAt) < unroutedGracePeriod {
		return
	}
	key := decision.CaseType + "|" + decision.Decision
	if _, seen := d.warned[key]; seen {
		return
	}
	d.warned[key] = struct{}{}
	slog.Warn("decision dispatcher: no domain adapter for decision",
		"caseType", decision.CaseType,
		"decision", decision.Decision,
		"caseId", decision.CaseID,
		"decisionId", decision.ID,
	)
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
		adapter := d.adapterFor(decision)
		if adapter == nil {
			d.warnUnrouted(decision)
			continue
		}
		if err := adapter.Apply(ctx, decision); err != nil {
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
