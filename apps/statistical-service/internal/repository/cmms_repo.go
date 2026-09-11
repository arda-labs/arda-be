package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CmmsResult is one CMMS compliance run (fe_statistical #21).
type CmmsResult struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	ScenarioCode     string          `json:"scenario_code"`
	CompliancePeriod string          `json:"compliance_period"`
	Status           string          `json:"status"`
	CheckedCount     int             `json:"checked_count"`
	FailedCount      int             `json:"failed_count"`
	Details          json.RawMessage `json:"details"`
	RunBy            string          `json:"run_by"`
	RunAt            time.Time       `json:"run_at"`
}

type ListCmmsResultsParams struct {
	TenantID         string
	ScenarioCode     string
	CompliancePeriod string
	Status           string
}

func (r *StatisticalRepository) ListCmmsResults(ctx context.Context, p ListCmmsResultsParams) ([]CmmsResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, scenario_code, compliance_period, status,
		       checked_count, failed_count, details, run_by, run_at
		FROM rpt_cmms_results
		WHERE tenant_id = $1
		  AND ($2 = '' OR scenario_code = $2)
		  AND ($3 = '' OR compliance_period = $3)
		  AND ($4 = '' OR status = $4)
		ORDER BY run_at DESC LIMIT 500`,
		p.TenantID, p.ScenarioCode, p.CompliancePeriod, p.Status)
	if err != nil {
		return nil, fmt.Errorf("list cmms results: %w", err)
	}
	defer rows.Close()

	items := []CmmsResult{}
	for rows.Next() {
		var it CmmsResult
		var details []byte
		if err := rows.Scan(&it.ID, &it.TenantID, &it.ScenarioCode, &it.CompliancePeriod, &it.Status,
			&it.CheckedCount, &it.FailedCount, &details, &it.RunBy, &it.RunAt); err != nil {
			return nil, err
		}
		it.Details = json.RawMessage(details)
		items = append(items, it)
	}
	return items, rows.Err()
}

func (r *StatisticalRepository) UpsertCmmsResult(ctx context.Context, in *CmmsResult) (*CmmsResult, error) {
	details := in.Details
	if len(details) == 0 {
		details = json.RawMessage("{}")
	}
	var out CmmsResult
	var raw []byte
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_cmms_results (
			tenant_id, scenario_code, compliance_period, status,
			checked_count, failed_count, details, run_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, scenario_code, compliance_period) DO UPDATE SET
			status = EXCLUDED.status, checked_count = EXCLUDED.checked_count,
			failed_count = EXCLUDED.failed_count, details = EXCLUDED.details,
			run_by = EXCLUDED.run_by, run_at = now()
		RETURNING id, tenant_id, scenario_code, compliance_period, status,
		          checked_count, failed_count, details, run_by, run_at`,
		in.TenantID, in.ScenarioCode, in.CompliancePeriod, in.Status,
		in.CheckedCount, in.FailedCount, string(details), in.RunBy,
	).Scan(&out.ID, &out.TenantID, &out.ScenarioCode, &out.CompliancePeriod, &out.Status,
		&out.CheckedCount, &out.FailedCount, &raw, &out.RunBy, &out.RunAt)
	if err != nil {
		return nil, fmt.Errorf("upsert cmms result: %w", err)
	}
	out.Details = json.RawMessage(raw)
	return &out, nil
}
