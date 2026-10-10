package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
)

// JobRunStore is the subset of the scheduled job repository the scheduler needs.
type JobRunStore interface {
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]repository.ClaimedRun, error)
	StartManual(ctx context.Context, code, actor string, params map[string]string, now time.Time) (*repository.ClaimedRun, error)
	Finalize(ctx context.Context, runID string, out domain.RunOutcome, now time.Time) error
	ReapAbandoned(ctx context.Context, now time.Time) (int, error)
}

const (
	schedulerTick     = 30 * time.Second
	reaperTick        = 60 * time.Second
	maxConcurrentJobs = 4
)

// JobScheduler claims due runs, executes them with bounded concurrency and
// records their outcome.
type JobScheduler struct {
	store    JobRunStore
	exec     *JobExecutor
	logger   *slog.Logger
	now      func() time.Time
	slots    chan struct{}
	wg       sync.WaitGroup
	lastTick atomic.Int64
	runsDone sync.Map // status -> *atomic.Int64
}

func NewJobScheduler(store JobRunStore, exec *JobExecutor, logger *slog.Logger) *JobScheduler {
	return &JobScheduler{store: store, exec: exec, logger: logger, now: time.Now, slots: make(chan struct{}, maxConcurrentJobs)}
}

// Tick claims only as many runs as there are free execution slots, so a busy
// replica leaves due jobs for its peers.
func (s *JobScheduler) Tick(ctx context.Context) int {
	s.lastTick.Store(s.now().Unix())
	free := cap(s.slots) - len(s.slots)
	if free <= 0 {
		return 0
	}
	claimed, err := s.store.ClaimDue(ctx, s.now(), free)
	if err != nil {
		s.logger.Error("scheduler claim failed", "err", err)
		return 0
	}
	for _, c := range claimed {
		s.dispatch(ctx, c)
	}
	return len(claimed)
}

func (s *JobScheduler) dispatch(ctx context.Context, c repository.ClaimedRun) {
	s.slots <- struct{}{}
	s.wg.Add(1)
	go func() {
		defer func() { <-s.slots; s.wg.Done() }()
		// Detach from the tick context: a run must be finalized even if the
		// tick that started it returns; shutdown waits via Wait().
		runCtx := context.WithoutCancel(ctx)
		out := s.exec.Execute(runCtx, c.Job, c.Run)
		if err := s.store.Finalize(runCtx, c.Run.ID, out, s.now()); err != nil {
			s.logger.Error("scheduler finalize failed", "run", c.Run.ID, "job", c.Job.Code, "err", err)
			return
		}
		s.countRun(out.Status)
		s.logger.Info("scheduled job finished", "job", c.Job.Code, "run", c.Run.ID, "status", out.Status, "attempt", out.Attempt, "error_code", out.ErrorCode)
	}()
}

// RunNow starts a manual run and returns it while it is still RUNNING.
func (s *JobScheduler) RunNow(ctx context.Context, code, actor string, params map[string]string) (*domain.ScheduledJobRun, error) {
	c, err := s.store.StartManual(ctx, code, actor, params, s.now())
	if err != nil {
		return nil, err
	}
	s.dispatch(ctx, *c)
	return &c.Run, nil
}

// Reap finalizes runs whose deadline has passed without a result.
func (s *JobScheduler) Reap(ctx context.Context) {
	if n, err := s.store.ReapAbandoned(ctx, s.now()); err != nil {
		s.logger.Error("scheduler reap failed", "err", err)
	} else if n > 0 {
		s.logger.Warn("scheduler reaped abandoned runs", "count", n)
	}
}

// Run drives Tick and Reap until ctx is cancelled.
func (s *JobScheduler) Run(ctx context.Context) {
	tick := time.NewTicker(schedulerTick)
	reap := time.NewTicker(reaperTick)
	defer tick.Stop()
	defer reap.Stop()
	s.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.Tick(ctx)
		case <-reap.C:
			s.Reap(ctx)
		}
	}
}

// Wait blocks until every dispatched run has been finalized.
func (s *JobScheduler) Wait() { s.wg.Wait() }

func (s *JobScheduler) countRun(status string) {
	v, _ := s.runsDone.LoadOrStore(status, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

// RenderMetrics writes the scheduler metrics in Prometheus text format.
func (s *JobScheduler) RenderMetrics(w io.Writer) {
	fmt.Fprintf(w, "# HELP arda_platform_scheduler_last_tick_timestamp_seconds Unix time of the last scheduler tick on this replica.\n# TYPE arda_platform_scheduler_last_tick_timestamp_seconds gauge\narda_platform_scheduler_last_tick_timestamp_seconds %d\n", s.lastTick.Load())
	fmt.Fprintf(w, "# HELP arda_platform_scheduled_job_runs_total Scheduled job runs finalized on this replica by status.\n# TYPE arda_platform_scheduled_job_runs_total counter\n")
	s.runsDone.Range(func(k, v any) bool {
		fmt.Fprintf(w, "arda_platform_scheduled_job_runs_total{status=%q} %d\n", k.(string), v.(*atomic.Int64).Load())
		return true
	})
}
