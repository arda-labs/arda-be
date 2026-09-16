package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

func newEODTestRepo() *mockCalendarRepo {
	current, _ := time.Parse("2006-01-02", "2026-09-15") // Tuesday
	next, _ := time.Parse("2006-01-02", "2026-09-16")    // Wednesday
	return &mockCalendarRepo{
		systemDate: &domain.SystemDate{
			ID:                   "sd_1",
			BranchCode:           "HEAD_OFFICE",
			CurrentBusinessDate:  current,
			PreviousBusinessDate: current.AddDate(0, 0, -1),
			NextBusinessDate:     next,
			Status:               domain.SystemDateOpen,
		},
	}
}

// A rejected claim (0 rows affected: another EOD run holds the row) must fail
// without touching the business date.
func TestRunEOD_ClaimConflictDoesNotAdvanceDate(t *testing.T) {
	repo := newEODTestRepo()
	repo.systemDate.Status = domain.SystemDateEODProcessing
	svc := NewCalendarService(repo)

	before := repo.systemDate.CurrentBusinessDate
	_, err := svc.RunEOD(context.Background(), "HEAD_OFFICE")
	if !errors.Is(err, domain.ErrEODInProgress) {
		t.Fatalf("expected ErrEODInProgress, got %v", err)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("date was written despite a rejected claim (updateCalls=%d)", repo.updateCalls)
	}
	if !repo.systemDate.CurrentBusinessDate.Equal(before) {
		t.Fatalf("business date advanced on a rejected claim: %s", repo.systemDate.CurrentBusinessDate)
	}
	if repo.releaseCalls != 0 {
		t.Fatalf("rejected claim must not release another run's gate (releaseCalls=%d)", repo.releaseCalls)
	}
}

func TestRunEOD_MissingSystemDate(t *testing.T) {
	repo := &mockCalendarRepo{}
	svc := NewCalendarService(repo)

	if _, err := svc.RunEOD(context.Background(), ""); !errors.Is(err, domain.ErrSystemDateNotFound) {
		t.Fatalf("expected ErrSystemDateNotFound, got %v", err)
	}
}

// A failure after the claim (e.g. a job error) must always put the row back to
// OPEN instead of leaving it stuck in EOD_PROCESSING.
func TestRunEOD_ReleasesProcessingStatusOnFailure(t *testing.T) {
	repo := newEODTestRepo()
	repo.isHolidayErr = errors.New("upstream calendar lookup failed")
	svc := NewCalendarService(repo)

	_, err := svc.RunEOD(context.Background(), "HEAD_OFFICE")
	if err == nil {
		t.Fatal("expected the holiday lookup error")
	}
	if repo.releaseCalls != 1 {
		t.Fatalf("releaseCalls = %d, want 1", repo.releaseCalls)
	}
	if repo.systemDate.Status != domain.SystemDateOpen {
		t.Fatalf("status = %s, want %s (must not stay stuck)", repo.systemDate.Status, domain.SystemDateOpen)
	}
}

// A failing final UPDATE (the old stuck-state case) must also release the gate.
func TestRunEOD_ReleasesProcessingStatusOnUpdateFailure(t *testing.T) {
	repo := newEODTestRepo()
	repo.updateErr = errors.New("db write failed")
	svc := NewCalendarService(repo)

	_, err := svc.RunEOD(context.Background(), "HEAD_OFFICE")
	if err == nil {
		t.Fatal("expected the final update error")
	}
	if repo.releaseCalls != 1 {
		t.Fatalf("releaseCalls = %d, want 1", repo.releaseCalls)
	}
	if repo.systemDate.Status != domain.SystemDateOpen {
		t.Fatalf("status = %s, want %s (must not stay stuck)", repo.systemDate.Status, domain.SystemDateOpen)
	}
}

func TestRunEOD_SuccessAdvancesOnce(t *testing.T) {
	repo := newEODTestRepo()
	svc := NewCalendarService(repo)

	sd, err := svc.RunEOD(context.Background(), "HEAD_OFFICE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sd.Status != domain.SystemDateOpen {
		t.Fatalf("status = %s, want OPEN", sd.Status)
	}
	if got := sd.CurrentBusinessDate.Format("2006-01-02"); got != "2026-09-16" {
		t.Fatalf("current business date = %s, want 2026-09-16", got)
	}
	if got := sd.PreviousBusinessDate.Format("2006-01-02"); got != "2026-09-15" {
		t.Fatalf("previous business date = %s, want 2026-09-15", got)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", repo.updateCalls)
	}
	if repo.releaseCalls != 0 {
		t.Fatalf("releaseCalls = %d, want 0 on success", repo.releaseCalls)
	}
}
