package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
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

// ErrSchedulerBusy means the replica has no free execution slot or is
// shutting down; the caller may retry later.
var ErrSchedulerBusy = errors.New("scheduler has no free execution slot")

const (
	schedulerTick     = 30 * time.Second
	reaperTick        = 60 * time.Second
	maxConcurrentJobs = 4
	finalizeTimeout   = 10 * time.Second

	finalizeErrorStatus = "finalize_error"
)

// JobScheduler claims due runs, executes them with bounded concurrency and
// records their outcome.
//
// Slot accounting: every run holds one slot in slots and one count in wg from
// reservation until its goroutine exits. Reservations happen under mu while
// checking closing, so wg.Add never races with the Wait in Shutdown.
type JobScheduler struct {
	store  JobRunStore
	exec   *JobExecutor
	logger *slog.Logger
	now    func() time.Time
	slots  chan struct{}
	wg     sync.WaitGroup

	mu      sync.Mutex
	closing bool

	// finalizeBackoff holds the waits between Finalize attempts; attempts =
	// len(finalizeBackoff)+1. Overridable in tests.
	finalizeBackoff []time.Duration

	lastTick    atomic.Int64
	claimErrors atomic.Int64
	runsDone    sync.Map // status -> *atomic.Int64
}

func NewJobScheduler(store JobRunStore, exec *JobExecutor, logger *slog.Logger) *JobScheduler {
	s := &JobScheduler{store: store, exec: exec, logger: logger, now: time.Now,
		slots:           make(chan struct{}, maxConcurrentJobs),
		finalizeBackoff: []time.Duration{time.Second, 2 * time.Second}}
	// Pre-register the series so rate() works from the first scrape.
	for _, st := range []string{domain.JobStatusSucceeded, domain.JobStatusFailed, domain.JobStatusTimedOut, finalizeErrorStatus} {
		s.runsDone.Store(st, new(atomic.Int64))
	}
	return s
}

// reserve takes up to max free slots without blocking and registers them in
// wg. closing reports that shutdown has started (n is then 0); n == 0 with
// closing == false means the replica is saturated.
func (s *JobScheduler) reserve(max int) (n int, closing bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return 0, true
	}
acquire:
	for n < max {
		select {
		case s.slots <- struct{}{}:
			n++
		default:
			break acquire
		}
	}
	s.wg.Add(n)
	return n, false
}

func (s *JobScheduler) release(n int) {
	for i := 0; i < n; i++ {
		<-s.slots
		s.wg.Done()
	}
}

// Tick claims only as many runs as there are free execution slots, so a busy
// replica leaves due jobs for its peers.
//
// The last-tick gauge is a liveness heartbeat: it advances after a successful
// claim and on a saturated tick (healthy, just busy), but not on a claim
// error or after Shutdown.
func (s *JobScheduler) Tick(ctx context.Context) int {
	reserved, closing := s.reserve(cap(s.slots))
	if closing {
		return 0
	}
	if reserved == 0 {
		s.lastTick.Store(s.now().Unix())
		return 0
	}
	claimed, err := s.store.ClaimDue(ctx, s.now(), reserved)
	if err != nil {
		s.release(reserved)
		s.claimErrors.Add(1)
		s.logger.Error("scheduler claim failed", "err", err)
		return 0
	}
	s.lastTick.Store(s.now().Unix())
	if len(claimed) > reserved { // defensive: never run more than reserved
		s.logger.Error("scheduler claim returned more runs than requested", "requested", reserved, "got", len(claimed))
		claimed = claimed[:reserved]
	}
	for _, c := range claimed {
		s.dispatch(ctx, c)
	}
	s.release(reserved - len(claimed))
	return len(claimed)
}

// dispatch runs c in a goroutine. The caller must already hold a reserved
// slot for it; the goroutine releases that slot when done.
func (s *JobScheduler) dispatch(ctx context.Context, c repository.ClaimedRun) {
	go func() {
		defer s.release(1)
		// Detach from the tick/request context: a run must be finalized even
		// if the caller returns; shutdown waits via Shutdown/Wait.
		runCtx := context.WithoutCancel(ctx)
		out := s.exec.Execute(runCtx, c.Job, c.Run)
		if err := s.finalize(runCtx, c.Run.ID, out); err != nil {
			s.countRun(finalizeErrorStatus)
			s.logger.Error("scheduler finalize failed", "run", c.Run.ID, "job", c.Job.Code, "status", out.Status, "err", err)
			return
		}
		s.countRun(out.Status)
		s.logger.Info("scheduled job finished", "job", c.Job.Code, "run", c.Run.ID, "status", out.Status, "attempt", out.Attempt, "error_code", out.ErrorCode)
	}()
}

// finalize retries transient store errors; each attempt has its own timeout.
func (s *JobScheduler) finalize(runCtx context.Context, runID string, out domain.RunOutcome) error {
	var err error
	for attempt := 0; attempt <= len(s.finalizeBackoff); attempt++ {
		if attempt > 0 {
			s.logger.Warn("scheduler finalize retry", "run", runID, "attempt", attempt+1, "err", err)
			time.Sleep(s.finalizeBackoff[attempt-1])
		}
		fctx, cancel := context.WithTimeout(runCtx, finalizeTimeout)
		err = s.store.Finalize(fctx, runID, out, s.now())
		cancel()
		if err == nil {
			return nil
		}
	}
	return err
}

// RunNow starts a manual run and returns it while it is still RUNNING. It
// never blocks on execution capacity: with no free slot (or during shutdown)
// it returns ErrSchedulerBusy without creating a run.
func (s *JobScheduler) RunNow(ctx context.Context, code, actor string, params map[string]string) (*domain.ScheduledJobRun, error) {
	if n, _ := s.reserve(1); n == 0 {
		return nil, ErrSchedulerBusy
	}
	c, err := s.store.StartManual(ctx, code, actor, params, s.now())
	if err != nil {
		s.release(1)
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

// Shutdown stops new dispatches (Tick claims nothing, RunNow returns
// ErrSchedulerBusy) and waits for in-flight runs until ctx is done.
func (s *JobScheduler) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wait blocks until every dispatched run has been finalized (tests).
func (s *JobScheduler) Wait() { s.wg.Wait() }

func (s *JobScheduler) countRun(status string) {
	v, _ := s.runsDone.LoadOrStore(status, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

// RenderMetrics writes the scheduler metrics in Prometheus text format.
func (s *JobScheduler) RenderMetrics(w io.Writer) {
	fmt.Fprintf(w, "# HELP arda_platform_scheduler_last_tick_timestamp_seconds Unix time of the last successful scheduler claim on this replica.\n# TYPE arda_platform_scheduler_last_tick_timestamp_seconds gauge\narda_platform_scheduler_last_tick_timestamp_seconds %d\n", s.lastTick.Load())
	fmt.Fprintf(w, "# HELP arda_platform_scheduler_claim_errors_total Scheduler ticks whose claim query failed on this replica.\n# TYPE arda_platform_scheduler_claim_errors_total counter\narda_platform_scheduler_claim_errors_total %d\n", s.claimErrors.Load())
	fmt.Fprintf(w, "# HELP arda_platform_scheduled_job_runs_total Scheduled job runs finalized on this replica by status.\n# TYPE arda_platform_scheduled_job_runs_total counter\n")
	var statuses []string
	s.runsDone.Range(func(k, _ any) bool {
		statuses = append(statuses, k.(string))
		return true
	})
	sort.Strings(statuses)
	for _, st := range statuses {
		v, _ := s.runsDone.Load(st)
		fmt.Fprintf(w, "arda_platform_scheduled_job_runs_total{status=%q} %d\n", st, v.(*atomic.Int64).Load())
	}
}
