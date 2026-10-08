package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
)

type CalendarRepository struct {
	db *sql.DB
}

func NewCalendarRepository(db *sql.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

func (r *CalendarRepository) GetSystemDate(ctx context.Context, branchCode string) (*domain.SystemDate, error) {
	if branchCode != "HEAD_OFFICE" {
		return nil, domain.ErrSystemDateNotFound
	}
	sd, err := r.BusinessDateForScope(ctx, ardaBusinessDate.Scope{Type: ardaBusinessDate.ScopeSystem})
	if errors.Is(err, domain.ErrSystemDateNotFound) {
		return nil, nil
	}
	return sd, err
}

// ClaimEOD moves the SYSTEM row into EOD_PROCESSING and returns it. The
// orchestrator's session advisory lock is the runner concurrency gate; allowing
// an already-processing row lets a new runner resume after a crashed process.
func (r *CalendarRepository) ClaimEOD(ctx context.Context, branchCode string) (*domain.SystemDate, error) {
	if branchCode != "HEAD_OFFICE" {
		return nil, domain.ErrSystemDateNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE plt_business_dates SET status=$1, updated_at=now() WHERE tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL AND status IN ($2,$3)", domain.SystemDateEODProcessing, domain.SystemDateOpen, domain.SystemDateEODProcessing)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		// No row matched: either the branch has no system date row or another
		// EOD run already claimed it. Read once to tell the caller which one.
		var exists bool
		if getErr := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM plt_business_dates WHERE tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL)").Scan(&exists); getErr != nil {
			return nil, getErr
		}
		if !exists {
			return nil, domain.ErrSystemDateNotFound
		}
		return nil, domain.ErrEODInProgress
	}
	if _, err := tx.ExecContext(ctx, "UPDATE plt_system_dates SET status=$1, updated_at=now() WHERE branch_code=$2", domain.SystemDateEODProcessing, branchCode); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	sd, err := r.GetSystemDate(ctx, branchCode)
	if err != nil {
		return nil, err
	}
	if sd == nil {
		return nil, domain.ErrSystemDateNotFound
	}
	return sd, nil
}

// ReleaseEOD clears the EOD_PROCESSING gate without touching business dates,
// so a failed job or DB error can never leave the branch stuck in
// EOD_PROCESSING. The status predicate keeps the release idempotent and
// harmless when the final transition already committed.
func (r *CalendarRepository) ReleaseEOD(ctx context.Context, branchCode string) error {
	if branchCode != "HEAD_OFFICE" {
		return domain.ErrSystemDateNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE plt_business_dates SET status=$1, updated_at=now() WHERE tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL AND status=$2", domain.SystemDateOpen, domain.SystemDateEODProcessing); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE plt_system_dates SET status=$1, updated_at=now() WHERE branch_code=$2 AND status=$3", domain.SystemDateOpen, branchCode, domain.SystemDateEODProcessing); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *CalendarRepository) CompleteEOD(ctx context.Context, expectedDate string, newCurrent, newNext time.Time) (*domain.SystemDate, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var item domain.SystemDate
	var lastEOD sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id, business_date, prev_business_date, next_business_date, status, last_eod_at, updated_at
		FROM plt_business_dates WHERE tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL FOR UPDATE`).
		Scan(&item.ID, &item.CurrentBusinessDate, &item.PreviousBusinessDate, &item.NextBusinessDate, &item.Status, &lastEOD, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if item.Status != domain.SystemDateEODProcessing || item.CurrentBusinessDate.Format("2006-01-02") != expectedDate {
		return nil, fmt.Errorf("SYSTEM business date is not locked for EOD date %s", expectedDate)
	}
	if newCurrent.IsZero() || newNext.IsZero() || !newNext.After(newCurrent) {
		return nil, fmt.Errorf("invalid next SYSTEM business date")
	}
	last := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `UPDATE plt_business_dates
		SET prev_business_date=business_date, business_date=$2, next_business_date=$3, status=$4, last_eod_at=$5, updated_at=now()
		WHERE id=$1`, item.ID, newCurrent, newNext, domain.SystemDateOpen, last)
	if err != nil {
		return nil, err
	}
	// Keep the legacy adapter row synchronized until T2.2's later drop
	// migration; its trigger mirrors the same committed transition.
	if _, err = tx.ExecContext(ctx, `UPDATE plt_system_dates SET previous_business_date=$2, current_business_date=$3,
		next_business_date=$4, status=$5, last_eod_at=$6, updated_at=now() WHERE id=$1`,
		item.ID, item.CurrentBusinessDate, newCurrent, newNext, domain.SystemDateOpen, last); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE plt_eod_runs SET status='SUCCEEDED',finished_at=now(),error=NULL
		WHERE eod_date=$1::date AND status='RUNNING'`, expectedDate)
	if err != nil {
		return nil, err
	}
	if affected, rowsErr := res.RowsAffected(); rowsErr != nil || affected != 1 {
		if rowsErr != nil {
			return nil, rowsErr
		}
		return nil, fmt.Errorf("SYSTEM EOD run is not RUNNING for date %s", expectedDate)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.BusinessDateForScope(ctx, ardaBusinessDate.Scope{Type: ardaBusinessDate.ScopeSystem})
}

func (r *CalendarRepository) IsHoliday(ctx context.Context, date time.Time) (bool, error) {
	return r.IsHolidayForScope(ctx, ardaBusinessDate.Scope{Type: ardaBusinessDate.ScopeSystem}, date)
}

func (r *CalendarRepository) AddHoliday(ctx context.Context, holiday *domain.HolidayCalendar) error {
	if holiday == nil {
		return errors.New("holiday is required")
	}
	if holiday.ID == "" {
		holiday.ID = NewID("holiday")
	}

	query := `
		INSERT INTO plt_holiday_calendars (id, holiday_date, description, is_recurring, holiday_year)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`
	var yearVal any
	if holiday.HolidayYear != nil {
		yearVal = *holiday.HolidayYear
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, query, holiday.ID, holiday.HolidayDate, holiday.Description, holiday.IsRecurring, yearVal).
		Scan(&holiday.CreatedAt); err != nil {
		return err
	}
	if err := addVersionedHolidayTx(ctx, tx, holiday); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *CalendarRepository) ListHolidays(ctx context.Context) ([]domain.HolidayCalendar, error) {
	query := `SELECT h.id, h.holiday_date, h.description, h.is_recurring, h.holiday_year, v.created_at
		FROM plt_working_calendar_holidays h
		JOIN plt_working_calendar_versions v ON v.id=h.calendar_version_id
		WHERE v.tenant_id IS NULL AND v.scope_type='SYSTEM' AND v.org_code IS NULL AND v.is_active
		ORDER BY h.holiday_date ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holidays []domain.HolidayCalendar
	for rows.Next() {
		var h domain.HolidayCalendar
		var yearVal sql.NullInt64
		if err := rows.Scan(&h.ID, &h.HolidayDate, &h.Description, &h.IsRecurring, &yearVal, &h.CreatedAt); err != nil {
			return nil, err
		}
		if yearVal.Valid {
			val := int(yearVal.Int64)
			h.HolidayYear = &val
		}
		holidays = append(holidays, h)
	}
	return holidays, nil
}

func (r *CalendarRepository) GetCutoffConfig(ctx context.Context, channelCode, txnType string) (*domain.CutoffConfig, error) {
	query := `
		SELECT id, channel_code, transaction_type, cutoff_time, is_active, updated_at
		FROM plt_cutoff_configs
		WHERE channel_code = $1 AND transaction_type = $2 AND is_active = TRUE
	`
	row := r.db.QueryRowContext(ctx, query, channelCode, txnType)

	var cc domain.CutoffConfig
	var rawTime string
	err := row.Scan(&cc.ID, &cc.ChannelCode, &cc.TransactionType, &rawTime, &cc.IsActive, &cc.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	cc.CutoffTime = rawTime
	return &cc, nil
}
