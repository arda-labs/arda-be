package domain

import "testing"

func TestCheckRegisterLimit(t *testing.T) {
	tests := []struct {
		name                      string
		loan, outstanding, amount int64
		wantErr                   bool
	}{
		{"fits headroom", 1_000, 400, 599, false},
		{"exactly at limit", 1_000, 400, 600, false},
		{"over limit", 1_000, 400, 601, true},
		{"empty contract", 500, 0, 500, false},
		{"fully drawn", 500, 500, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckRegisterLimit(tt.loan, tt.outstanding, tt.amount); (err != nil) != tt.wantErr {
				t.Fatalf("CheckRegisterLimit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckCompleteRemainder(t *testing.T) {
	tests := []struct {
		name                        string
		register, completed, amount int64
		wantErr                     bool
	}{
		{"fits remainder", 500, 100, 399, false},
		{"exactly at remainder", 500, 100, 400, false},
		{"exceeds remainder", 500, 100, 401, true},
		{"second drawdown", 500, 500, 1, true},
		{"first drawdown full", 500, 0, 500, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckCompleteRemainder(tt.register, tt.completed, tt.amount); (err != nil) != tt.wantErr {
				t.Fatalf("CheckCompleteRemainder() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
