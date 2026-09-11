package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// Rank-score runtime (fe_statistical #19). Indicators + weights are configured
// in the mdm scoring catalogs; the FE resolves the criteria-mapping and posts
// the entries + benchmark bands here, so no scoring SQL expression ever runs
// (the EPAS SQL_CALC_RESULT pitfall we deliberately dropped).

// ScoreEntry is one scored indicator: value out of max_value with a weight.
type ScoreEntry struct {
	IndicatorCode string  `json:"indicator_code"`
	Value         float64 `json:"value"`
	MaxValue      float64 `json:"max_value"`
	Weight        float64 `json:"weight"`
}

// ScoreBand is one benchmark band from mdm scoring-benchmarks.
type ScoreBand struct {
	RankCode string  `json:"rank_code"`
	MinScore float64 `json:"min_score"`
}

type ScoreInput struct {
	ScoringTypeCode string
	SubjectType     string
	SubjectRef      string
	Entries         []ScoreEntry
	Bands           []ScoreBand
}

// ComputeScore normalises each indicator to 0..1, weights it, scales the
// weighted average to 100, then picks the highest benchmark band at or below
// the total. Pure function (unit-tested).
func ComputeScore(entries []ScoreEntry, bands []ScoreBand) (float64, float64, string, error) {
	if len(entries) == 0 {
		return 0, 0, "", fmt.Errorf("at least one indicator entry is required")
	}
	var weighted, weightSum float64
	for i, e := range entries {
		if strings.TrimSpace(e.IndicatorCode) == "" {
			return 0, 0, "", fmt.Errorf("entry %d: indicator_code is required", i+1)
		}
		if e.MaxValue <= 0 {
			return 0, 0, "", fmt.Errorf("entry %d: max_value must be > 0", i+1)
		}
		if e.Value < 0 {
			return 0, 0, "", fmt.Errorf("entry %d: value must be >= 0", i+1)
		}
		if e.Weight < 0 {
			return 0, 0, "", fmt.Errorf("entry %d: weight must be >= 0", i+1)
		}
		weightSum += e.Weight
		weighted += e.Weight * (e.Value / e.MaxValue)
	}
	if weightSum <= 0 {
		return 0, 0, "", fmt.Errorf("total weight must be > 0")
	}
	total := math.Round(100*weighted/weightSum*10000) / 10000

	rank := ""
	best := math.Inf(-1)
	for _, b := range bands {
		if total >= b.MinScore && b.MinScore >= best {
			best = b.MinScore
			rank = b.RankCode
		}
	}
	return total, 100, rank, nil
}

// CreateScoreResult computes and persists one ranking result.
func (s *StatisticalService) CreateScoreResult(ctx context.Context, tenantID, actor string, in ScoreInput) (*repository.ScoreResult, error) {
	if strings.TrimSpace(in.ScoringTypeCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "scoring_type_code is required")
	}
	total, maxScore, rank, err := ComputeScore(in.Entries, in.Bands)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInvalidInput, err.Error())
	}
	subjectType := strings.TrimSpace(in.SubjectType)
	if subjectType == "" {
		subjectType = "CUSTOMER"
	}
	raw, err := json.Marshal(in.Entries)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeInternal, err.Error())
	}
	return s.repo.CreateScoreResult(ctx, &repository.ScoreResult{
		TenantID:        tenantID,
		ScoringTypeCode: strings.TrimSpace(in.ScoringTypeCode),
		SubjectType:     subjectType,
		SubjectRef:      strings.TrimSpace(in.SubjectRef),
		TotalScore:      total,
		MaxScore:        maxScore,
		RankCode:        rank,
		Indicators:      raw,
		CreatedBy:       actor,
	})
}

func (s *StatisticalService) ListScoreResults(ctx context.Context, p repository.ListScoreResultsParams) ([]repository.ScoreResult, error) {
	return s.repo.ListScoreResults(ctx, p)
}

func (s *StatisticalService) GetScoreResult(ctx context.Context, tenantID, id string) (*repository.ScoreResult, error) {
	item, ok, err := s.repo.GetScoreResult(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "score result not found")
	}
	return item, nil
}

// ListImportTransactions returns staged QCMS import transactions.
func (s *StatisticalService) ListImportTransactions(ctx context.Context, p repository.ListImportTransactionsParams) ([]repository.ImportTransaction, error) {
	return s.repo.ListImportTransactions(ctx, p)
}

// UpsertImportTransaction stages (or updates) an import transaction.
func (s *StatisticalService) UpsertImportTransaction(ctx context.Context, tenantID, actor string, in *repository.ImportTransaction) (*repository.ImportTransaction, error) {
	if strings.TrimSpace(in.ImportTypeCode) == "" || strings.TrimSpace(in.PeriodCode) == "" {
		return nil, ardaerrors.New(ardaerrors.CodeRequired, "import_type_code and period_code are required")
	}
	in.TenantID = tenantID
	in.CreatedBy = actor
	if strings.TrimSpace(in.Status) == "" {
		in.Status = "STAGED"
	}
	if in.RowCount < 0 {
		in.RowCount = 0
	}
	return s.repo.UpsertImportTransaction(ctx, in)
}

// SubmitImportTransaction marks a staged import POSTED.
func (s *StatisticalService) SubmitImportTransaction(ctx context.Context, tenantID, id, actor string) (*repository.ImportTransaction, error) {
	item, err := s.repo.SetImportTransactionStatus(ctx, tenantID, id, "POSTED", actor)
	if err != nil {
		return nil, ardaerrors.New(ardaerrors.CodeNotFound, "import transaction not found")
	}
	return item, nil
}
