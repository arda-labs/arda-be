package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

type CalendarRepo interface {
	GetSystemDate(ctx context.Context, branchCode string) (*domain.SystemDate, error)
	UpdateSystemDate(ctx context.Context, sd *domain.SystemDate) error
	IsHoliday(ctx context.Context, date time.Time) (bool, error)
	AddHoliday(ctx context.Context, holiday *domain.HolidayCalendar) error
	ListHolidays(ctx context.Context) ([]domain.HolidayCalendar, error)
	GetCutoffConfig(ctx context.Context, channelCode, txnType string) (*domain.CutoffConfig, error)
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

// RunEOD performs the End-Of-Day transition: shifts business dates forward.
func (s *CalendarService) RunEOD(ctx context.Context, branchCode string) (*domain.SystemDate, error) {
	if branchCode == "" {
		branchCode = "HEAD_OFFICE"
	}

	sd, err := s.repo.GetSystemDate(ctx, branchCode)
	if err != nil {
		return nil, err
	}
	if sd == nil {
		return nil, errors.New("system date config not found")
	}

	if sd.Status == domain.SystemDateEODProcessing {
		return nil, errors.New("EOD process is already in progress")
	}

	sd.Status = domain.SystemDateEODProcessing
	if err := s.repo.UpdateSystemDate(ctx, sd); err != nil {
		return nil, fmt.Errorf("failed to update status to EOD_PROCESSING: %w", err)
	}

	slog.Info("EOD process started", "branch", branchCode, "currentBusinessDate", sd.CurrentBusinessDate)

	// Simulate EOD reconciliation tasks
	time.Sleep(50 * time.Millisecond)

	sd.PreviousBusinessDate = sd.CurrentBusinessDate
	sd.CurrentBusinessDate = sd.NextBusinessDate

	nextDay, err := s.calculateNextBusinessDay(ctx, sd.CurrentBusinessDate)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate next business day: %w", err)
	}
	sd.NextBusinessDate = nextDay

	nowTime := time.Now()
	sd.LastEODAt = &nowTime
	sd.Status = domain.SystemDateOpen

	if err := s.repo.UpdateSystemDate(ctx, sd); err != nil {
		return nil, fmt.Errorf("failed to complete EOD transition in DB: %w", err)
	}

	slog.Info("EOD process completed successfully", "newBusinessDate", sd.CurrentBusinessDate, "nextBusinessDate", sd.NextBusinessDate)
	return sd, nil
}

func (s *CalendarService) calculateNextBusinessDay(ctx context.Context, start time.Time) (time.Time, error) {
	nextDay := start.AddDate(0, 0, 1)
	for {
		// Skip weekends (Saturday = 6, Sunday = 0)
		if nextDay.Weekday() == time.Saturday || nextDay.Weekday() == time.Sunday {
			nextDay = nextDay.AddDate(0, 0, 1)
			continue
		}

		// Skip holidays
		isHoliday, err := s.repo.IsHoliday(ctx, nextDay)
		if err != nil {
			return time.Time{}, err
		}
		if isHoliday {
			nextDay = nextDay.AddDate(0, 0, 1)
			continue
		}

		break
	}
	return nextDay, nil
}
