package service

import "testing"

func TestComputeScoreWeightedBands(t *testing.T) {
	entries := []ScoreEntry{
		{IndicatorCode: "FIN_DEBT_GROUP", Value: 8, MaxValue: 10, Weight: 0.3},
		{IndicatorCode: "FIN_PROFIT", Value: 10, MaxValue: 10, Weight: 0.3},
		{IndicatorCode: "COLL_VALUE", Value: 4, MaxValue: 10, Weight: 0.4},
	}
	bands := []ScoreBand{
		{RankCode: "C", MinScore: 0},
		{RankCode: "B", MinScore: 60},
		{RankCode: "A", MinScore: 80},
	}
	// weighted = 0.3*0.8 + 0.3*1 + 0.4*0.4 = 0.24+0.3+0.16 = 0.70 → 70
	total, maxScore, rank, err := ComputeScore(entries, bands)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if total != 70 {
		t.Fatalf("total = %v, want 70", total)
	}
	if maxScore != 100 {
		t.Fatalf("max = %v, want 100", maxScore)
	}
	if rank != "B" {
		t.Fatalf("rank = %q, want B", rank)
	}
}

func TestComputeScoreNormalisesWeights(t *testing.T) {
	// Weights need not sum to 1: 90/100 with weights 1 and 1 → 90.
	total, _, rank, err := ComputeScore(
		[]ScoreEntry{
			{IndicatorCode: "A", Value: 9, MaxValue: 10, Weight: 1},
			{IndicatorCode: "B", Value: 9, MaxValue: 10, Weight: 1},
		},
		[]ScoreBand{{RankCode: "A", MinScore: 80}},
	)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if total != 90 || rank != "A" {
		t.Fatalf("total=%v rank=%q, want 90/A", total, rank)
	}
}

func TestComputeScoreRejectsBadInput(t *testing.T) {
	cases := [][]ScoreEntry{
		{},
		{{IndicatorCode: "A", Value: 1, MaxValue: 0, Weight: 1}},
		{{IndicatorCode: "", Value: 1, MaxValue: 10, Weight: 1}},
		{{IndicatorCode: "A", Value: 1, MaxValue: 10, Weight: 0}},
		{{IndicatorCode: "A", Value: -1, MaxValue: 10, Weight: 1}},
	}
	for i, entries := range cases {
		if _, _, _, err := ComputeScore(entries, nil); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}
