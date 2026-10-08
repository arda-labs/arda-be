package repository

import (
	"context"
	"database/sql"
	"errors"
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

// ClaimEOD atomically moves a branch into EOD_PROCESSING and returns the
// claimed row. The conditional UPDATE plus RowsAffected is the concurrency
// gate: only one caller can transition a row out of a non-processing status,
// so two parallel triggers can never both pass and advance the business date
// twice (the previous GetSystemDate -> check -> UpdateSystemDate sequence
// could). A rejected claim reports whether the row is missing or already
// processing.
func (r *CalendarRepository) ClaimEOD(ctx context.Context, branchCode string) (*domain.SystemDate, error) {
	if branchCode != "HEAD_OFFICE" {
		return nil, domain.ErrSystemDateNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "UPDATE plt_business_dates SET status=$1, updated_at=now() WHERE tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL AND status=$2", domain.SystemDateEODProcessing, domain.SystemDateOpen)
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

func (r *CalendarRepository) UpdateSystemDate(ctx context.Context, sd *domain.SystemDate) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lastEOD any
	if sd.LastEODAt != nil {
		lastEOD = *sd.LastEODAt
	}
	canonical, err := tx.ExecContext(ctx, "UPDATE plt_business_dates SET business_date=$2, prev_business_date=$3, next_business_date=$4, status=$5, last_eod_at=$6, updated_at=now() WHERE id=$1 AND tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL",
		sd.ID, sd.CurrentBusinessDate, sd.PreviousBusinessDate, sd.NextBusinessDate, sd.Status, lastEOD)
	if err != nil {
		return err
	}
	rows, err := canonical.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return domain.ErrSystemDateNotFound
	}
	_, err = tx.ExecContext(ctx, "UPDATE plt_system_dates SET current_business_date=$2, previous_business_date=$3, next_business_date=$4, status=$5, last_eod_at=$6, updated_at=now() WHERE id=$1",
		sd.ID,
		sd.CurrentBusinessDate,
		sd.PreviousBusinessDate,
		sd.NextBusinessDate,
		sd.Status,
		lastEOD,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
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
