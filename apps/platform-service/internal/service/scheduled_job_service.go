package service

import (
	"context"
	"errors"
	"slices"
	"time"

	ardapostgres "github.com/arda-labs/arda/libs/go/arda-postgres"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
)

// Sentinel errors the HTTP handler maps to problem codes. Domain validation
// errors (domain.ErrInvalidCron, ErrInvalidTimezone, ErrFieldOutOfRange) and
// ErrSchedulerBusy are returned unchanged.
var (
	ErrJobNotFound            = errors.New("scheduled job not found")
	ErrJobVersionConflict     = errors.New("scheduled job changed since it was read")
	ErrJobAlreadyRunning      = errors.New("scheduled job is already running")
	ErrJobTargetNotConfigured = errors.New("scheduled job target is not configured")
	ErrJobParamNotAllowed     = errors.New("parameter not allowed for this job")
	ErrJobRetired             = errors.New("scheduled job is retired")
)

// ScheduledJobService is the application layer behind the scheduled job API.
type ScheduledJobService struct {
	repo          *repository.ScheduledJobRepository
	scheduler     *JobScheduler
	exec          *JobExecutor
	defaultTenant string
}

func NewScheduledJobService(repo *repository.ScheduledJobRepository, scheduler *JobScheduler, exec *JobExecutor, defaultTenant string) *ScheduledJobService {
	return &ScheduledJobService{repo: repo, scheduler: scheduler, exec: exec, defaultTenant: defaultTenant}
}

func mapRepoErr(err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return ErrJobNotFound
	case errors.Is(err, ardapostgres.ErrStaleVersion):
		return ErrJobVersionConflict
	case errors.Is(err, repository.ErrJobAlreadyRunning):
		return ErrJobAlreadyRunning
	}
	return err
}

func (s *ScheduledJobService) decorate(ctx context.Context, j *domain.ScheduledJob) error {
	j.TargetConfigured = s.exec.Configured(j.OwnerService)
	runs, _, err := s.repo.ListRuns(ctx, j.Code, "", 1, 0)
	if err != nil {
		return err
	}
	if len(runs) == 1 {
		j.LastRun = &runs[0]
	}
	return nil
}

func (s *ScheduledJobService) List(ctx context.Context) ([]domain.ScheduledJob, error) {
	jobs, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		if err := s.decorate(ctx, &jobs[i]); err != nil {
			return nil, err
		}
	}
	return jobs, nil
}

func (s *ScheduledJobService) Get(ctx context.Context, code string) (*domain.ScheduledJob, error) {
	j, err := s.repo.Get(ctx, code)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if err := s.decorate(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

func (s *ScheduledJobService) Update(ctx context.Context, code string, in domain.JobConfigUpdate, tenantID, actor string) (*domain.ScheduledJob, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	current, err := s.repo.Get(ctx, code)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if in.Enabled && current.RetiredAt != nil {
		return nil, ErrJobRetired
	}
	if in.Enabled && in.Cron() != "" && !s.exec.Configured(current.OwnerService) {
		return nil, ErrJobTargetNotConfigured
	}
	if tenantID == "" {
		tenantID = s.defaultTenant
	}
	j, err := s.repo.UpdateConfig(ctx, code, in, tenantID, actor, time.Now())
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if err := s.decorate(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

// RunNow starts a manual run. A disabled job may still be run by hand; a
// retired one may not. ErrSchedulerBusy from the scheduler passes through.
func (s *ScheduledJobService) RunNow(ctx context.Context, code, actor string, params map[string]string) (*domain.ScheduledJobRun, error) {
	job, err := s.repo.Get(ctx, code)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	def, ok := domain.FindJobDefinition(code)
	if job.RetiredAt != nil || !ok {
		return nil, ErrJobRetired
	}
	for k := range params {
		if !slices.Contains(def.AllowedParams, k) {
			return nil, ErrJobParamNotAllowed
		}
	}
	if !s.exec.Configured(job.OwnerService) {
		return nil, ErrJobTargetNotConfigured
	}
	run, err := s.scheduler.RunNow(ctx, code, actor, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return run, nil
}

func (s *ScheduledJobService) Preview(expr, tz string) ([]time.Time, error) {
	return domain.PreviewRuns(expr, tz, time.Now(), 5)
}

func (s *ScheduledJobService) ListRuns(ctx context.Context, code, status string, limit, offset int) ([]domain.ScheduledJobRun, int, error) {
	return s.repo.ListRuns(ctx, code, status, limit, offset)
}

func (s *ScheduledJobService) GetRun(ctx context.Context, id string) (*domain.ScheduledJobRun, error) {
	r, err := s.repo.GetRun(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return r, nil
}
