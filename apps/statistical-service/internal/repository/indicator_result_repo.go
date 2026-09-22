package repository

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// UpsertIndicatorResult writes one computed/manual value for an indicator in a
// period, recording the previous value in the cell audit when it changed. The
// write is idempotent on (tenant, indicator, period, dimension_key, revision):
// re-running a period overwrites the value but never loses the audit trail.
func (r *StatisticalRepository) UpsertIndicatorResult(ctx context.Context, res *IndicatorResult) (*IndicatorResult, error) {
	if res.Revision <= 0 {
		res.Revision = 1
	}
	if res.Source == "" {
		res.Source = "COMPUTED"
	}

	var oldValue sql.NullFloat64
	if err := r.db.QueryRowContext(ctx, `
		SELECT value FROM rpt_indicator_results
		WHERE tenant_id = $1 AND indicator_code = $2 AND period_code = $3
		  AND dimension_key = $4 AND revision = $5`,
		res.TenantID, res.IndicatorCode, res.PeriodCode, res.DimensionKey, res.Revision,
	).Scan(&oldValue); err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// The table id is UUID DEFAULT uuidv7(); let the database generate it.
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_indicator_results
			(tenant_id, indicator_code, period_code, dimension_key, business_date,
			 value, revision, source, created_by)
		VALUES ($1,$2,$3,$4, NULLIF($5,'')::date, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, indicator_code, period_code, dimension_key, revision)
		DO UPDATE SET value = EXCLUDED.value, business_date = EXCLUDED.business_date,
			source = EXCLUDED.source, created_by = EXCLUDED.created_by,
			updated_at = now(), version = rpt_indicator_results.version + 1
		RETURNING id::text, created_at, updated_at`,
		res.TenantID, res.IndicatorCode, res.PeriodCode, res.DimensionKey,
		res.BusinessDate, res.Value, res.Revision, res.Source, res.CreatedBy)
	if err := row.Scan(&res.ID, &res.CreatedAt, &res.UpdatedAt); err != nil {
		return nil, err
	}

	// Cell audit: only record when a prior value existed and changed.
	if oldValue.Valid && (res.Value == nil || *res.Value != oldValue.Float64) {
		if _, err := r.db.ExecContext(ctx, `
			INSERT INTO rpt_indicator_audit
				(tenant_id, indicator_code, period_code, dimension_key, old_value, new_value, reason, actor)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			res.TenantID, res.IndicatorCode, res.PeriodCode, res.DimensionKey,
			oldValue.Float64, res.Value, "", res.CreatedBy); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// ListIndicatorResultsParams carries the result listing filters.
type ListIndicatorResultsParams struct {
	TenantID      string
	PeriodCode    string
	IndicatorCode string
	Revision      int
}

// ListIndicatorResults returns stored indicator values for a period, newest
// revision first.
func (r *StatisticalRepository) ListIndicatorResults(ctx context.Context, params ListIndicatorResultsParams) ([]IndicatorResult, error) {
	where := []string{"tenant_id = $1"}
	args := []any{params.TenantID}
	if params.PeriodCode != "" {
		args = append(args, params.PeriodCode)
		where = append(where, "period_code = $"+itoa(len(args)))
	}
	if params.IndicatorCode != "" {
		args = append(args, params.IndicatorCode)
		where = append(where, "indicator_code = $"+itoa(len(args)))
	}
	if params.Revision > 0 {
		args = append(args, params.Revision)
		where = append(where, "revision = $"+itoa(len(args)))
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, indicator_code, period_code, dimension_key,
		       COALESCE(business_date::text, ''), value, revision, source,
		       COALESCE(created_by, ''), created_at, updated_at
		FROM rpt_indicator_results
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY period_code DESC, indicator_code, dimension_key, revision DESC`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IndicatorResult{}
	for rows.Next() {
		var x IndicatorResult
		if err := rows.Scan(&x.ID, &x.TenantID, &x.IndicatorCode, &x.PeriodCode, &x.DimensionKey,
			&x.BusinessDate, &x.Value, &x.Revision, &x.Source, &x.CreatedBy,
			&x.CreatedAt, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
