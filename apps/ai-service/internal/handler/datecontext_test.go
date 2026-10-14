package handler

import (
	"strings"
	"testing"
	"time"
)

func TestDateContextPrompt(t *testing.T) {
	loc := time.FixedZone("Asia/Ho_Chi_Minh", 7*3600)
	cases := []struct {
		name string
		now  time.Time
		want []string
	}{
		{
			name: "mid year",
			now:  time.Date(2026, time.October, 14, 9, 30, 0, 0, loc),
			want: []string{"2026-10-14", "periodCode 2026-10", "\"tháng trước\" = 2026-09", "Q4/2026", "= 2026."},
		},
		{
			name: "january rolls the previous month into last year",
			now:  time.Date(2027, time.January, 3, 8, 0, 0, 0, loc),
			want: []string{"2027-01-03", "periodCode 2027-01", "\"tháng trước\" = 2026-12", "Q1/2027"},
		},
		{
			name: "month end does not skip a month",
			now:  time.Date(2026, time.March, 31, 23, 0, 0, 0, loc),
			want: []string{"periodCode 2026-03", "\"tháng trước\" = 2026-02"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dateContextPrompt(tc.now)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("prompt missing %q:\n%s", want, got)
				}
			}
			if !strings.Contains(got, "Asia/Ho_Chi_Minh") {
				t.Errorf("prompt does not name the timezone:\n%s", got)
			}
		})
	}
}
