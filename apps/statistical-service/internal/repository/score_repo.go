package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ScoreResult is one persisted QCMS ranking result (fe_statistical rank-score).
type ScoreResult struct {
	ID              string          `json:"id"`
	TenantID        string          `json:"tenant_id"`
	ScoringTypeCode string          `json:"scoring_type_code"`
	SubjectType     string          `json:"subject_type"`
	SubjectRef      string          `json:"subject_ref"`
	TotalScore      float64         `json:"total_score"`
	MaxScore        float64         `json:"max_score"`
	RankCode        string          `json:"rank_code"`
	Indicators      json.RawMessage `json:"indicators"`
	CreatedBy       string          `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
}

type ListScoreResultsParams struct {
	TenantID        string
	ScoringTypeCode string
	SubjectRef      string
}

func (r *StatisticalRepository) CreateScoreResult(ctx context.Context, in *ScoreResult) (*ScoreResult, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("database not available")
	}
	indicators := in.Indicators
	if len(indicators) == 0 {
		indicators = json.RawMessage("[]")
	}
	var out ScoreResult
	var raw []byte
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO rpt_score_results (
			tenant_id, scoring_type_code, subject_type, subject_ref,
			total_score, max_score, rank_code, indicators, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, tenant_id, scoring_type_code, subject_type, subject_ref,
		          total_score, max_score, rank_code, indicators, created_by, created_at`,
		in.TenantID, in.ScoringTypeCode, in.SubjectType, in.SubjectRef,
		in.TotalScore, in.MaxScore, in.RankCode, string(indicators), in.CreatedBy,
	).Scan(&out.ID, &out.TenantID, &out.ScoringTypeCode, &out.SubjectType, &out.SubjectRef,
		&out.TotalScore, &out.MaxScore, &out.RankCode, &raw, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert score result: %w", err)
	}
	out.Indicators = json.RawMessage(raw)
	return &out, nil
}

func (r *StatisticalRepository) ListScoreResults(ctx context.Context, p ListScoreResultsParams) ([]ScoreResult, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("database not available")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, scoring_type_code, subject_type, subject_ref,
		       total_score, max_score, rank_code, indicators, created_by, created_at
		FROM rpt_score_results
		WHERE tenant_id = $1
		  AND ($2 = '' OR scoring_type_code = $2)
		  AND ($3 = '' OR subject_ref = $3)
		ORDER BY created_at DESC
		LIMIT 200`, p.TenantID, p.ScoringTypeCode, p.SubjectRef)
	if err != nil {
		return nil, fmt.Errorf("list score results: %w", err)
	}
	defer rows.Close()

	items := []ScoreResult{}
	for rows.Next() {
		var it ScoreResult
		var raw []byte
		if err := rows.Scan(&it.ID, &it.TenantID, &it.ScoringTypeCode, &it.SubjectType, &it.SubjectRef,
			&it.TotalScore, &it.MaxScore, &it.RankCode, &raw, &it.CreatedBy, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.Indicators = json.RawMessage(raw)
		items = append(items, it)
	}
	return items, rows.Err()
}

func (r *StatisticalRepository) GetScoreResult(ctx context.Context, tenantID, id string) (*ScoreResult, bool, error) {
	if r == nil || r.db == nil {
		return nil, false, errors.New("database not available")
	}
	var it ScoreResult
	var raw []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, scoring_type_code, subject_type, subject_ref,
		       total_score, max_score, rank_code, indicators, created_by, created_at
		FROM rpt_score_results
		WHERE tenant_id = $1 AND id = $2`, tenantID, id).
		Scan(&it.ID, &it.TenantID, &it.ScoringTypeCode, &it.SubjectType, &it.SubjectRef,
			&it.TotalScore, &it.MaxScore, &it.RankCode, &raw, &it.CreatedBy, &it.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get score result: %w", err)
	}
	it.Indicators = json.RawMessage(raw)
	return &it, true, nil
}
