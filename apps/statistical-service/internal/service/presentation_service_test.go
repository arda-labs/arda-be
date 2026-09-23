package service

import "testing"

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
