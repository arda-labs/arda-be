package domain

import "testing"

func TestRequiredSpecificProvision(t *testing.T) {
	tests := []struct {
		name string
		base int64
		rate float64
		want int64
	}{
		{name: "zero base", base: 0, rate: 5},
		{name: "zero rate", base: 100_000_000, rate: 0},
		{name: "exact amount", base: 1_000_000_000, rate: 5, want: 50_000_000},
		{name: "half-up rounding", base: 333_333_333, rate: 0.75, want: 2_500_000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RequiredSpecificProvision(tc.base, tc.rate); got != tc.want {
				t.Fatalf("RequiredSpecificProvision(%d, %v) = %d, want %d", tc.base, tc.rate, got, tc.want)
			}
		})
	}
}

func TestSpecificProvisionDelta(t *testing.T) {
	tests := []struct {
		name                  string
		required, current     int64
		wantAdd, wantReversal int64
	}{
		{name: "increase", required: 150, current: 100, wantAdd: 50},
		{name: "decrease", required: 80, current: 100, wantReversal: 20},
		{name: "unchanged", required: 100, current: 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			add, reversal := SpecificProvisionDelta(tc.required, tc.current)
			if add != tc.wantAdd || reversal != tc.wantReversal {
				t.Fatalf("SpecificProvisionDelta(%d, %d) = (%d, %d), want (%d, %d)", tc.required, tc.current, add, reversal, tc.wantAdd, tc.wantReversal)
			}
		})
	}
}
