package service

import "testing"

func TestRequiredGeneralProvision(t *testing.T) {
	cases := []struct {
		name        string
		outstanding int64
		rate        float64
		want        int64
	}{
		{"zero outstanding", 0, 0.75, 0},
		{"zero rate", 100_000_000, 0, 0},
		{"exact 0.75 percent", 200_000_000_000, 0.75, 1_500_000_000},
		{"half-up rounding", 333_333_333, 0.75, 2_500_000},
		{"five percent", 1_000_000_000, 5, 50_000_000},
	}
	for _, tc := range cases {
		if got := requiredGeneralProvision(tc.outstanding, tc.rate); got != tc.want {
			t.Fatalf("%s: requiredGeneralProvision(%d, %v) = %d, want %d",
				tc.name, tc.outstanding, tc.rate, got, tc.want)
		}
	}
}

func TestGeneralProvisionDelta(t *testing.T) {
	cases := []struct {
		required, accum, alloc, reverse int64
	}{
		{100, 40, 60, 0},
		{40, 100, 0, 60},
		{70, 70, 0, 0},
		{0, 0, 0, 0},
	}
	for _, tc := range cases {
		alloc, reverse := generalProvisionDelta(tc.required, tc.accum)
		if alloc != tc.alloc || reverse != tc.reverse {
			t.Fatalf("delta(%d, %d) = (%d, %d), want (%d, %d)",
				tc.required, tc.accum, alloc, reverse, tc.alloc, tc.reverse)
		}
	}
}
