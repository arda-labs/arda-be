package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
)

type CalendarRepo interface {
	GetSystemDate(ctx context.Context, branchCode string) (*domain.SystemDate, error)
	ClaimEOD(ctx context.Context, branchCode string) (*domain.SystemDate, error)
	ReleaseEOD(ctx context.Context, branchCode string) error
	IsHoliday(ctx context.Context, date time.Time) (bool, error)
	AddHoliday(ctx context.Context, holiday *domain.HolidayCalendar) error
	ListHolidays(ctx context.Context) ([]domain.HolidayCalendar, error)
	GetCutoffConfig(ctx context.Context, channelCode, txnType string) (*domain.CutoffConfig, error)
	BusinessDateForScope(ctx context.Context, scope ardaBusinessDate.Scope) (*domain.SystemDate, error)
	CurrentBusinessDate(ctx context.Context, scope ardaBusinessDate.Scope) (time.Time, error)
	IsHolidayForScope(ctx context.Context, scope ardaBusinessDate.Scope, date time.Time) (bool, error)
}

type CalendarService struct {
	repo CalendarRepo
}

func NewCalendarService(repo CalendarRepo) *CalendarService {
	return &CalendarService{repo: repo}
}

func (s *CalendarService) GetSystemDate(ctx context.Context, branchCode string) (*domain.SystemDate, error) {
	if branchCode == "" {
		branchCode = "HEAD_OFFICE"
	}
	return s.repo.GetSystemDate(ctx, branchCode)
}

func (s *CalendarService) BusinessDateForScope(ctx context.Context, scope ardaBusinessDate.Scope) (*domain.SystemDate, error) {
	if scope.Type != ardaBusinessDate.ScopeSystem && scope.Type != ardaBusinessDate.ScopeOrg {
		return nil, fmt.Errorf("unsupported business-date scope %q", scope.Type)
	}
	return s.repo.BusinessDateForScope(ctx, scope)
}

func (s *CalendarService) CurrentBusinessDate(ctx context.Context, scope ardaBusinessDate.Scope) (time.Time, error) {
	return s.repo.CurrentBusinessDate(ctx, scope)
}

func (s *CalendarService) IsHolidayForScope(ctx context.Context, scope ardaBusinessDate.Scope, date time.Time) (bool, error) {
	return s.repo.IsHolidayForScope(ctx, scope, date)
}

func (s *CalendarService) IsHoliday(ctx context.Context, scope ardaBusinessDate.Scope, date time.Time) (bool, error) {
	return s.IsHolidayForScope(ctx, scope, date)
}

func (s *CalendarService) AddHoliday(ctx context.Context, date time.Time, description string, recurring bool) (*domain.HolidayCalendar, error) {
	holiday := &domain.HolidayCalendar{
		HolidayDate: date,
		Description: description,
		IsRecurring: recurring,
	}
	if !recurring {
		year := date.Year()
		holiday.HolidayYear = &year
	}
	err := s.repo.AddHoliday(ctx, holiday)
	if err != nil {
		return nil, err
	}
	return holiday, nil
}

func (s *CalendarService) ListHolidays(ctx context.Context) ([]domain.HolidayCalendar, error) {
	return s.repo.ListHolidays(ctx)
}

// EvaluateAccountingDate determines the correct business accounting date for a transaction based on cut-off config.
func (s *CalendarService) EvaluateAccountingDate(ctx context.Context, branchCode string, channelCode, txnType string, executionTime time.Time) (time.Time, error) {
	sd, err := s.GetSystemDate(ctx, branchCode)
	if err != nil {
		return time.Time{}, err
	}
	if sd == nil {
		return time.Time{}, errors.New("system date not initialized")
	}

	cutoff, err := s.repo.GetCutoffConfig(ctx, channelCode, txnType)
	if err != nil {
		return time.Time{}, err
	}

	if cutoff == nil {
		return sd.CurrentBusinessDate, nil
	}

	t, err := time.Parse("15:04:05", cutoff.CutoffTime)
	if err != nil {
		t, err = time.Parse("15:04", cutoff.CutoffTime)
		if err != nil {
			slog.Error("failed to parse cutoff time config", "config", cutoff.CutoffTime, "err", err)
			return sd.CurrentBusinessDate, nil
		}
	}

	cutoffTimeToday := time.Date(
		executionTime.Year(), executionTime.Month(), executionTime.Day(),
		t.Hour(), t.Minute(), t.Second(), 0, executionTime.Location(),
	)

	if executionTime.After(cutoffTimeToday) {
		slog.Info("transaction execution time is after cutoff, routing to next business date",
			"executionTime", executionTime, "cutoffTime", cutoffTimeToday, "nextBusinessDate", sd.NextBusinessDate)
		return sd.NextBusinessDate, nil
	}

	return sd.CurrentBusinessDate, nil
}

// BeginEOD atomically closes the global SYSTEM date to new posting while the
// orchestrator executes its required tenant steps.
func (s *CalendarService) BeginEOD(ctx context.Context) (*domain.SystemDate, error) {
	return s.repo.ClaimEOD(ctx, "HEAD_OFFICE")
}

func (s *CalendarService) ReleaseEOD(ctx context.Context) error {
	return s.repo.ReleaseEOD(ctx, "HEAD_OFFICE")
}

// CompleteEOD advances the canonical SYSTEM date only after every required
// step succeeded. The date comparison protects against stale orchestrators.
func (s *CalendarService) CompleteEOD(ctx context.Context, expectedDate string) (*domain.SystemDate, error) {
	sd, err := s.repo.BusinessDateForScope(ctx, ardaBusinessDate.Scope{Type: ardaBusinessDate.ScopeSystem})
	if err != nil {
		return nil, err
	}
	if sd.CurrentBusinessDate.Format("2006-01-02") != expectedDate {
		return nil, fmt.Errorf("SYSTEM business date changed during EOD: expected %s, found %s", expectedDate, sd.CurrentBusinessDate.Format("2006-01-02"))
	}
	newCurrent := sd.NextBusinessDate
	newNext, err := s.calculateNextBusinessDay(ctx, newCurrent)
	if err != nil {
		return nil, fmt.Errorf("calculate next SYSTEM business date: %w", err)
	}
	completer, ok := s.repo.(interface {
		CompleteEOD(context.Context, string, time.Time, time.Time) (*domain.SystemDate, error)
	})
	if !ok {
		return nil, fmt.Errorf("calendar repository does not support atomic EOD completion")
	}
	return completer.CompleteEOD(ctx, expectedDate, newCurrent, newNext)
}

func (s *CalendarService) calculateNextBusinessDay(ctx context.Context, start time.Time) (time.Time, error) {
	return ardaBusinessDate.NextWorkingDay(ctx, repositoryCalendar{repo: s.repo}, ardaBusinessDate.Scope{Type: ardaBusinessDate.ScopeSystem}, start)
}

type repositoryCalendar struct{ repo CalendarRepo }

func (c repositoryCalendar) CurrentBusinessDate(ctx context.Context, scope ardaBusinessDate.Scope) (time.Time, error) {
	return c.repo.CurrentBusinessDate(ctx, scope)
}

func (c repositoryCalendar) IsHoliday(ctx context.Context, scope ardaBusinessDate.Scope, date time.Time) (bool, error) {
	return c.repo.IsHolidayForScope(ctx, scope, date)
}
