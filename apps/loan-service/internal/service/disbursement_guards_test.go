package service

import "testing"

// Pure guard tests for the two-flow disbursement limits — the DB-backed
// versions live in TestDisbursementSmoke (LOAN_SMOKE_DSN gated).

func TestCheckRegisterLimit(t *testing.T) {
	tests := []struct {
		name          string
		contractLoan  int64
		outstanding   int64
		disburse      int64
		wantOverLimit bool
	}{
		{"fits headroom", 1_000_000_000, 400_000_000, 600_000_000 - 1, false},
		{"exactly at limit", 1_000_000_000, 400_000_000, 600_000_000, false},
		{"over limit", 1_000_000_000, 400_000_000, 600_000_001, true},
		{"empty contract", 500_000_000, 0, 500_000_000, false},
		{"fully drawn", 500_000_000, 500_000_000, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkRegisterLimit(tt.contractLoan, tt.outstanding, tt.disburse)
			if got := err != nil; got != tt.wantOverLimit {
				t.Fatalf("checkRegisterLimit(%d, %d, %d) err = %v, wantOverLimit %v",
					tt.contractLoan, tt.outstanding, tt.disburse, err, tt.wantOverLimit)
			}
		})
	}
}

func TestCheckCompleteRemainder(t *testing.T) {
	tests := []struct {
		name        string
		registerAmt int64
		completed   int64
		disburse    int64
		wantReject  bool
	}{
		{"fits remainder", 500_000_000, 100_000_000, 400_000_000 - 1, false},
		{"exactly at remainder", 500_000_000, 100_000_000, 400_000_000, false},
		{"exceeds remainder", 500_000_000, 100_000_000, 400_000_001, true},
		{"second drawdown", 500_000_000, 500_000_000, 1, true},
		{"first drawdown full", 500_000_000, 0, 500_000_000, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCompleteRemainder(tt.registerAmt, tt.completed, tt.disburse)
			if got := err != nil; got != tt.wantReject {
				t.Fatalf("checkCompleteRemainder(%d, %d, %d) err = %v, wantReject %v",
					tt.registerAmt, tt.completed, tt.disburse, err, tt.wantReject)
			}
		})
	}
}
