package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
)

func (r *CalendarRepository) BusinessDateForScope(ctx context.Context, scope ardaBusinessDate.Scope) (*domain.SystemDate, error) {
	if err := ardaBusinessDate.ValidateScope(scope); err != nil {
		return nil, err
	}
	if scope.Type == ardaBusinessDate.ScopeOrg {
		return nil, domain.ErrBusinessDateScopeMappingRequired
	}
	var item domain.SystemDate
	var lastEOD sql.NullTime
	err := r.db.QueryRowContext(ctx, "SELECT id, 'HEAD_OFFICE', business_date, prev_business_date, next_business_date, status, last_eod_at, updated_at "+
		"FROM plt_business_dates WHERE tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL").
		Scan(&item.ID, &item.BranchCode, &item.CurrentBusinessDate, &item.PreviousBusinessDate, &item.NextBusinessDate, &item.Status, &lastEOD, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrSystemDateNotFound
	}
	if err != nil {
		return nil, err
	}
	if lastEOD.Valid {
		item.LastEODAt = &lastEOD.Time
	}
	return &item, nil
}

func (r *CalendarRepository) CurrentBusinessDate(ctx context.Context, scope ardaBusinessDate.Scope) (time.Time, error) {
	item, err := r.BusinessDateForScope(ctx, scope)
	if err != nil {
		return time.Time{}, err
	}
	return item.CurrentBusinessDate, nil
}

func (r *CalendarRepository) IsHolidayForScope(ctx context.Context, scope ardaBusinessDate.Scope, date time.Time) (bool, error) {
	if err := ardaBusinessDate.ValidateScope(scope); err != nil {
		return false, err
	}
	if scope.Type == ardaBusinessDate.ScopeOrg {
		return false, domain.ErrBusinessDateScopeMappingRequired
	}
	var holiday bool
	err := r.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM plt_working_calendar_holidays h "+
		"JOIN plt_working_calendar_versions v ON v.id=h.calendar_version_id "+
		"WHERE v.tenant_id IS NULL AND v.scope_type='SYSTEM' AND v.org_code IS NULL AND v.is_active "+
		"AND ((h.holiday_date=$1 AND NOT h.is_recurring) OR (h.is_recurring AND EXTRACT(MONTH FROM h.holiday_date)=$2 AND EXTRACT(DAY FROM h.holiday_date)=$3)))",
		date.Format("2006-01-02"), int(date.Month()), date.Day()).Scan(&holiday)
	return holiday, err
}

func addVersionedHolidayTx(ctx context.Context, tx *sql.Tx, holiday *domain.HolidayCalendar) error {
	var previousID string
	var version int
	err := tx.QueryRowContext(ctx, "SELECT id, version FROM plt_working_calendar_versions WHERE tenant_id IS NULL AND scope_type='SYSTEM' AND org_code IS NULL AND is_active FOR UPDATE").Scan(&previousID, &version)
	if err != nil {
		return err
	}
	newID := NewID("calendar")
	if _, err := tx.ExecContext(ctx, "UPDATE plt_working_calendar_versions SET is_active=false WHERE id=$1", previousID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO plt_working_calendar_versions(id,tenant_id,scope_type,org_code,version,is_active) VALUES($1,NULL,'SYSTEM',NULL,$2,true)", newID, version+1); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO plt_working_calendar_holidays(calendar_version_id,id,holiday_date,description,is_recurring,holiday_year) "+
		"SELECT $1,id,holiday_date,description,is_recurring,holiday_year FROM plt_working_calendar_holidays WHERE calendar_version_id=$2", newID, previousID); err != nil {
		return err
	}
	var year any
	if holiday.HolidayYear != nil {
		year = *holiday.HolidayYear
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO plt_working_calendar_holidays(calendar_version_id,id,holiday_date,description,is_recurring,holiday_year) "+
		"VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(calendar_version_id,holiday_date) DO UPDATE SET description=EXCLUDED.description,is_recurring=EXCLUDED.is_recurring,holiday_year=EXCLUDED.holiday_year",
		newID, holiday.ID, holiday.HolidayDate, holiday.Description, holiday.IsRecurring, year); err != nil {
		return err
	}
	return nil
}
