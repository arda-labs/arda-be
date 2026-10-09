package domain

import "testing"

func TestDaysBetween(t *testing.T) {
	tests := []struct {
		from, to string
		want     int
		wantErr  bool
	}{
		{"2026-01-01", "2026-01-31", 30, false},
		{"2026-02-28", "2026-03-01", 1, false},
		{"2026-03-02", "2026-03-01", -1, false},
		{"bad", "2026-03-01", 0, true},
		{"2026-03-01", "bad", 0, true},
	}
	for _, tt := range tests {
		got, err := DaysBetween(tt.from, tt.to)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("DaysBetween(%q, %q) = (%d, %v), want (%d, wantErr=%v)", tt.from, tt.to, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestAccruedInterestMinor(t *testing.T) {
	tests := []struct {
		name      string
		principal int64
		rate      float64
		days      int
		currency  string
		want      int64
	}{
		{"full month", 1_000_000, 12, 30, "VND", 10_000},
		{"half month", 1_000_000, 12, 15, "VND", 5_000},
		{"default currency", 1_000_000, 12, 15, "", 5_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AccruedInterestMinor(tt.principal, tt.rate, tt.days, tt.currency)
			if err != nil || got != tt.want {
				t.Fatalf("AccruedInterestMinor() = (%d, %v), want (%d, nil)", got, err, tt.want)
			}
		})
	}
}
