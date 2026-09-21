package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
	"github.com/arda-labs/arda/apps/workflow-service/internal/service"
)

// WorkflowReconciler closes workflow_tasks rows whose Zeebe user task already
// reached a terminal state but the DB row was left open (failed complete-write,
// engine change outside the handler, restart, …). It is intentionally
// conservative: it only closes a row when the engine state is explicitly
// terminal, or when the key is absent while another user task of the same
// process is still CREATED (which proves the exporter is current).
type WorkflowReconciler struct {
	repo     *repository.CaseRepository
	rest     *service.ZeebeRestClient
	interval time.Duration
	grace    time.Duration
}

func NewWorkflowReconciler(repo *repository.CaseRepository, rest *service.ZeebeRestClient) *WorkflowReconciler {
	if repo == nil {
		return nil
	}
	return &WorkflowReconciler{repo: repo, rest: rest, interval: 2 * time.Minute, grace: 2 * time.Minute}
}

func (w *WorkflowReconciler) Run(ctx context.Context) {
	if w == nil {
		return
	}
	slog.Info("workflow reconciler started", "interval", w.interval.String(), "grace", w.grace.String())
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("workflow reconciler stopped")
			return
		case <-ticker.C:
			w.reconcileOnce(ctx)
		}
	}
}

func (w *WorkflowReconciler) reconcileOnce(ctx context.Context) {
	candidates, err := w.repo.ListReconcileCandidates(ctx, int(w.grace.Seconds()), 200)
	if err != nil {
		slog.Warn("workflow reconciler: list candidates failed", "err", err)
		return
	}
	if len(candidates) == 0 {
		return
	}
	if w.rest == nil || !w.rest.Enabled() {
		return
	}

	byProcess := make(map[int64][]repository.ReconcileCandidate)
	for _, candidate := range candidates {
		byProcess[candidate.ProcessInstanceKey] = append(byProcess[candidate.ProcessInstanceKey], candidate)
	}

	for processInstanceKey, rows := range byProcess {
		states, err := w.rest.UserTaskStates(ctx, processInstanceKey)
		if err != nil {
			slog.Debug("workflow reconciler: engine state skipped",
				"processInstanceKey", processInstanceKey, "err", err)
			continue
		}
		hasActive := false
		for _, state := range states {
			if state == "CREATED" {
				hasActive = true
				break
			}
		}
		for _, row := range rows {
			state, seen := states[row.JobKey]
			if reconcileAction(state, seen, hasActive) == reconcileClose {
				status, err := w.repo.ReconcileWorkItem(ctx, row.ID)
				if err != nil {
					slog.Warn("workflow reconciler: close failed", "taskId", row.ID, "err", err)
					continue
				}
				slog.Info("workflow reconciler closed stale task",
					"taskId", row.ID,
					"caseId", row.CaseID,
					"elementStatus", row.Status,
					"engineState", state,
					"dbStatus", status,
				)
				continue
			}
			if err := w.repo.TouchWorkItemChecked(ctx, row.ID); err != nil {
				slog.Debug("workflow reconciler: touch failed", "taskId", row.ID, "err", err)
			}
		}
	}
}

const (
	reconcileTouch = "touch"
	reconcileClose = "close"
)

// reconcileAction decides whether an open DB row should be closed:
//   - engine state CREATED → still active (touch only)
//   - engine state COMPLETED / CANCELED → close
//   - key absent but the process still has a CREATED task → the exporter is
//     current, so the missing key is terminal → close
//   - otherwise → touch only (never close on incomplete engine data)
func reconcileAction(state string, seen, hasActive bool) string {
	switch {
	case seen && state == "CREATED":
		return reconcileTouch
	case seen && (state == "COMPLETED" || state == "CANCELED"):
		return reconcileClose
	case !seen && hasActive:
		return reconcileClose
	default:
		return reconcileTouch
	}
}
