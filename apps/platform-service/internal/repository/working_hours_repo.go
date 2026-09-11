package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// WorkingHour is one weekly working shift (W6a).
type WorkingHour struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	OrgCode      string    `json:"org_code,omitempty"`
	DayOfWeek    int       `json:"day_of_week"`
	StartTime    string    `json:"start_time"`
	EndTime      string    `json:"end_time"`
	BreakMinutes int       `json:"break_minutes"`
	IsActive     bool      `json:"is_active"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ListWorkingHours returns the weekly shifts for one org scope.
func (r *PlatformRepository) ListWorkingHours(ctx context.Context, tenantID, orgCode string) ([]WorkingHour, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id::text, tenant_id, org_code, day_of_week, start_time::text, end_time::text,
		       break_minutes, is_active, COALESCE(created_by,''), created_at, updated_at
		FROM plt_working_hours WHERE tenant_id = $1 AND ($2 = '' OR org_code = $2)
		ORDER BY day_of_week`, tenantID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkingHour{}
	for rows.Next() {
		var w WorkingHour
		if err := rows.Scan(&w.ID, &w.TenantID, &w.OrgCode, &w.DayOfWeek, &w.StartTime, &w.EndTime,
			&w.BreakMinutes, &w.IsActive, &w.CreatedBy, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// UpsertWorkingHour creates or updates one shift (org + day unique).
func (r *PlatformRepository) UpsertWorkingHour(ctx context.Context, in *WorkingHour) (*WorkingHour, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO plt_working_hours (tenant_id, org_code, day_of_week, start_time, end_time,
			break_minutes, is_active, created_by)
		VALUES ($1,$2,$3,$4::time,$5::time,$6,COALESCE($7,true),$8)
		ON CONFLICT (tenant_id, org_code, day_of_week) DO UPDATE SET
			start_time = EXCLUDED.start_time, end_time = EXCLUDED.end_time,
			break_minutes = EXCLUDED.break_minutes, is_active = EXCLUDED.is_active,
			updated_at = now(), version = plt_working_hours.version + 1
		RETURNING id::text, created_at, updated_at`,
		in.TenantID, in.OrgCode, in.DayOfWeek, in.StartTime, in.EndTime, in.BreakMinutes, in.IsActive, in.CreatedBy)
	if err := row.Scan(&in.ID, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	return in, nil
}

// SetWorkingHourActive toggles one shift by id.
func (r *PlatformRepository) SetWorkingHourActive(ctx context.Context, tenantID, id string, active bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE plt_working_hours SET is_active = $3, updated_at = now(), version = version + 1
		WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id, active)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("working hour not found")
	}
	return nil
}

var _ = sql.ErrNoRows
