package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
)

type CalendarRepository struct {
	db *sql.DB
}

func NewCalendarRepository(db *sql.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

func (r *CalendarRepository) GetSystemDate(ctx context.Context, branchCode string) (*domain.SystemDate, error) {
	query := `
		SELECT id, branch_code, current_business_date, previous_business_date, next_business_date, status, last_eod_at, updated_at
		FROM plt_system_dates
		WHERE branch_code = $1
	`
	row := r.db.QueryRowContext(ctx, query, branchCode)

	var sd domain.SystemDate
	var lastEOD sql.NullTime
	err := row.Scan(&sd.ID, &sd.BranchCode, &sd.CurrentBusinessDate, &sd.PreviousBusinessDate, &sd.NextBusinessDate, &sd.Status, &lastEOD, &sd.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if lastEOD.Valid {
		sd.LastEODAt = &lastEOD.Time
	}

	return &sd, nil
}

func (r *CalendarRepository) UpdateSystemDate(ctx context.Context, sd *domain.SystemDate) error {
	query := `
		UPDATE plt_system_dates
		SET current_business_date = $2,
		    previous_business_date = $3,
		    next_business_date = $4,
		    status = $5,
		    last_eod_at = $6,
		    updated_at = now()
		WHERE id = $1
	`
	var lastEOD any
	if sd.LastEODAt != nil {
		lastEOD = *sd.LastEODAt
	}

	_, err := r.db.ExecContext(ctx, query,
		sd.ID,
		sd.CurrentBusinessDate,
		sd.PreviousBusinessDate,
		sd.NextBusinessDate,
		sd.Status,
		lastEOD,
	)
	return err
}

func (r *CalendarRepository) IsHoliday(ctx context.Context, date time.Time) (bool, error) {
	targetDate := date.Format("2006-01-02")
	parsedDate, err := time.Parse("2006-01-02", targetDate)
	if err != nil {
		return false, err
	}

	query := `
		SELECT EXISTS (
			SELECT 1 FROM plt_holiday_calendars
			WHERE (holiday_date = $1 AND is_recurring = FALSE)
			   OR (is_recurring = TRUE AND EXTRACT(MONTH FROM holiday_date) = $2 AND EXTRACT(DAY FROM holiday_date) = $3)
		)
	`
	var exists bool
	err = r.db.QueryRowContext(ctx, query, parsedDate, parsedDate.Month(), parsedDate.Day()).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *CalendarRepository) AddHoliday(ctx context.Context, holiday *domain.HolidayCalendar) error {
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

	return r.db.QueryRowContext(ctx, query, holiday.ID, holiday.HolidayDate, holiday.Description, holiday.IsRecurring, yearVal).
		Scan(&holiday.CreatedAt)
}

func (r *CalendarRepository) ListHolidays(ctx context.Context) ([]domain.HolidayCalendar, error) {
	query := `SELECT id, holiday_date, description, is_recurring, holiday_year, created_at FROM plt_holiday_calendars ORDER BY holiday_date ASC`
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
