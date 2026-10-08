package service

import (
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

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
			add, reversal := repository.SpecificProvisionDelta(tc.required, tc.current)
			if add != tc.wantAdd || reversal != tc.wantReversal {
				t.Fatalf("specificProvisionDelta(%d, %d) = (%d, %d), want (%d, %d)",
					tc.required, tc.current, add, reversal, tc.wantAdd, tc.wantReversal)
			}
		})
	}
}

func TestSpecificProvisionPostingLegs(t *testing.T) {
	analytics := &financev1.Analytics{ContractCode: "C-1"}
	tests := []struct {
		name       string
		delta      int64
		lineOne    int32
		lineTwo    int32
		amountWant int64
	}{
		{name: "increase", delta: 50, lineOne: 1, lineTwo: 2, amountWant: 50},
		{name: "reversal", delta: -20, lineOne: 3, lineTwo: 4, amountWant: 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			legs := specificProvisionPostingLegs(tc.delta, analytics)
			if len(legs) != 2 || legs[0].CardLine != tc.lineOne || legs[1].CardLine != tc.lineTwo ||
				legs[0].AmountMinor != tc.amountWant || legs[1].AmountMinor != tc.amountWant {
				t.Fatalf("posting legs = %#v", legs)
			}
			wantDirection := "DEBIT"
			if legs[1].Direction != "CREDIT" || legs[0].Direction != wantDirection {
				t.Fatalf("posting directions = %s/%s, want DEBIT/CREDIT", legs[0].Direction, legs[1].Direction)
			}
		})
	}
	if got := specificProvisionPostingLegs(0, analytics); len(got) != 0 {
		t.Fatalf("zero delta legs = %#v", got)
	}
}
