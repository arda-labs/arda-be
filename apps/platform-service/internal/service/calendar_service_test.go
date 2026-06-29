package service

import (
	"context"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

type mockCalendarRepo struct {
	systemDate *domain.SystemDate
	holidays   map[string]bool // Specific year holidays format: "YYYY-MM-DD"
	recurring  map[string]bool // Recurring holidays format: "MM-DD"
	cutoff     *domain.CutoffConfig
}

func (m *mockCalendarRepo) GetSystemDate(ctx context.Context, branchCode string) (*domain.SystemDate, error) {
	return m.systemDate, nil
}

func (m *mockCalendarRepo) UpdateSystemDate(ctx context.Context, sd *domain.SystemDate) error {
	m.systemDate = sd
	return nil
}

func (m *mockCalendarRepo) IsHoliday(ctx context.Context, date time.Time) (bool, error) {
	// 1. Check specific date
	if m.holidays[date.Format("2006-01-02")] {
		return true, nil
	}
	// 2. Check recurring date
	if m.recurring[date.Format("01-02")] {
		return true, nil
	}
	return false, nil
}

func (m *mockCalendarRepo) AddHoliday(ctx context.Context, holiday *domain.HolidayCalendar) error {
	return nil
}

func (m *mockCalendarRepo) ListHolidays(ctx context.Context) ([]domain.HolidayCalendar, error) {
	return nil, nil
}

func (m *mockCalendarRepo) GetCutoffConfig(ctx context.Context, channelCode, txnType string) (*domain.CutoffConfig, error) {
	return m.cutoff, nil
}

func TestCalculateNextBusinessDay_RecurringAndSpecific(t *testing.T) {
	ctx := context.Background()

	mockRepo := &mockCalendarRepo{
		holidays: map[string]bool{
			"2026-06-30": true, // Specific holiday in 2026
		},
		recurring: map[string]bool{
			"09-02": true, // September 2nd (National Day)
		},
	}
	svc := NewCalendarService(mockRepo)

	// Test 1: From 2026-06-29 (Monday) -> Tuesday 30th is specific holiday -> Should skip to Wednesday 2026-07-01
	monday, _ := time.Parse("2006-01-02", "2026-06-29")
	nextDay, err := svc.calculateNextBusinessDay(ctx, monday)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nextDay.Format("2006-01-02") != "2026-07-01" {
		t.Errorf("expected 2026-07-01, got %s", nextDay.Format("2006-01-02"))
	}

	// Test 2: In 2027, June 30th is NOT a holiday (since 2026-06-30 was specific).
	// Monday 2027-06-28 -> should transition to Tuesday 2027-06-29.
	monday2027, _ := time.Parse("2006-01-02", "2027-06-28")
	nextDay, err = svc.calculateNextBusinessDay(ctx, monday2027)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nextDay.Format("2006-01-02") != "2027-06-29" {
		t.Errorf("expected 2027-06-29, got %s", nextDay.Format("2006-01-02"))
	}

	// Test 3: September 2nd is recurring (National Day).
	// In 2026, Sep 1st is Tuesday. Sep 2nd is Wednesday. Should skip Sep 2nd to Sep 3rd.
	sep1, _ := time.Parse("2006-01-02", "2026-09-01")
	nextDay, err = svc.calculateNextBusinessDay(ctx, sep1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nextDay.Format("2006-01-02") != "2026-09-03" {
		t.Errorf("expected 2026-09-03, got %s", nextDay.Format("2006-01-02"))
	}
}
