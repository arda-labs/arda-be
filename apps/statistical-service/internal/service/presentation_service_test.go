package service

import (
	"testing"

	"github.com/arda-labs/arda/apps/statistical-service/internal/repository"
)

func TestReportKpiFilter(t *testing.T) {
	cases := []struct {
		name         string
		reportGroup  string
		wantFiltered bool
		wantContains string
		wantEmpty    bool
	}{
		{name: "loan maps to credit", reportGroup: "LNM", wantFiltered: true, wantContains: "Tín dụng"},
		{name: "case-insensitive", reportGroup: "dpm", wantFiltered: true, wantContains: "Huy động vốn"},
		{name: "operations has no group", reportGroup: "OPS", wantFiltered: true, wantEmpty: true},
		{name: "empty is unfiltered", reportGroup: "", wantFiltered: false},
		{name: "unknown is unfiltered", reportGroup: "ZZZ", wantFiltered: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			groups, filtered := reportKpiFilter(tc.reportGroup)
			if filtered != tc.wantFiltered {
				t.Fatalf("filtered = %v, want %v", filtered, tc.wantFiltered)
			}
			if tc.wantEmpty && len(groups) != 0 {
				t.Fatalf("expected empty groups, got %v", groups)
			}
			if tc.wantContains != "" && !containsString(groups, tc.wantContains) {
				t.Fatalf("groups %v should contain %q", groups, tc.wantContains)
			}
		})
	}
}

// A capped KPI list must keep the base risk indicators: ordering by indicator
// code alone put every growth/average derivation before dư nợ xấu.
func TestRankKpiCardsKeepsRiskIndicators(t *testing.T) {
	mk := func(code, group string) repository.Indicator {
		return repository.Indicator{Code: code, Name: code, GroupCode: group}
	}
	byCode := map[string]repository.Indicator{
		"10000.01":    mk("10000.01", "Huy động vốn"),
		"30000.01":    mk("30000.01", "Tín dụng"),
		"30000.01.02": mk("30000.01.02", "Tín dụng"),
		"30000.01.03": mk("30000.01.03", "Tín dụng"),
		"30001.01":    mk("30001.01", "Tín dụng"),
		"30002.01":    mk("30002.01", "Tín dụng"),
		"30002.01.02": mk("30002.01.02", "Tín dụng"),
		"30002.01.03": mk("30002.01.03", "Tín dụng"),
		"30002.02":    mk("30002.02", "Tín dụng"),
		"30002.02.02": mk("30002.02.02", "Tín dụng"),
		"30020.01":    mk("30020.01", "Tín dụng"),
		"30020.02":    mk("30020.02", "Tín dụng"),
	}
	codes := []string{
		"10000.01", "30000.01", "30000.01.02", "30000.01.03", "30001.01",
		"30002.01", "30002.01.02", "30002.01.03", "30002.02", "30002.02.02",
		"30020.01", "30020.02",
	}
	results := make([]repository.IndicatorResult, 0, len(codes)+1)
	for _, code := range codes {
		results = append(results, repository.IndicatorResult{IndicatorCode: code})
	}
	// A dimension slice must never become a headline card.
	results = append(results, repository.IndicatorResult{IndicatorCode: "30020.01", DimensionKey: "P"})

	kpi := rankKpiCards(results, byCode, []string{"Tín dụng"}, true, maxReportKPI)
	if len(kpi) != maxReportKPI {
		t.Fatalf("cards = %d, want %d", len(kpi), maxReportKPI)
	}
	has := func(code string) bool {
		for _, card := range kpi {
			if card.Code == code {
				return true
			}
		}
		return false
	}
	if !has("30020.01") || !has("30020.02") {
		t.Fatalf("risk KPIs dropped from the capped list: %+v", kpi)
	}
	if has("10000.01") {
		t.Fatal("indicator from another business group leaked into the report")
	}
	if kpi[0].Code != "30000.01" {
		t.Fatalf("first card = %s, want 30000.01", kpi[0].Code)
	}
}
