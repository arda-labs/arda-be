package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestParsePostingStrategy(t *testing.T) {
	for _, code := range []string{"SIMPLE", "BAL_TYPE_SPLIT", "DEBT_GROUP_RECLASS"} {
		strategy, err := ParsePostingStrategy(code)
		if err != nil || string(strategy) != code {
			t.Fatalf("ParsePostingStrategy(%q) = %q, %v", code, strategy, err)
		}
	}
	if _, err := ParsePostingStrategy("UNKNOWN"); !errors.Is(err, ErrUnknownPostingStrategy) {
		t.Fatalf("unknown strategy error = %v, want ErrUnknownPostingStrategy", err)
	}
}

func TestDebtGroupMatrixPreservesBalancesAndSupportsReverse(t *testing.T) {
	matrix, err := NewDebtGroupMatrix([]DebtGroupTransition{{From: "A", To: "B"}, {From: "B", To: "A"}})
	if err != nil {
		t.Fatal(err)
	}
	initial := map[string]int64{"A": 700, "B": 300}
	forward, err := matrix.Apply(initial, []DebtGroupTransfer{{From: "A", To: "B", AmountMinor: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(forward, map[string]int64{"A": 500, "B": 500}) {
		t.Fatalf("forward balances = %v", forward)
	}
	reverse, err := matrix.Apply(forward, []DebtGroupTransfer{{From: "B", To: "A", AmountMinor: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reverse, initial) {
		t.Fatalf("reverse balances = %v, want %v", reverse, initial)
	}
	if !reflect.DeepEqual(initial, map[string]int64{"A": 700, "B": 300}) {
		t.Fatalf("Apply mutated input balances: %v", initial)
	}
}

func TestDebtGroupMatrixRejectsUnconfiguredOrInvalidTransfers(t *testing.T) {
	matrix, err := NewDebtGroupMatrix([]DebtGroupTransition{{From: "A", To: "B"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, transfer := range []DebtGroupTransfer{
		{From: "A", To: "C", AmountMinor: 1},
		{From: "A", To: "B", AmountMinor: 0},
		{From: "A", To: "B", AmountMinor: 101},
	} {
		if _, err := matrix.Apply(map[string]int64{"A": 100}, []DebtGroupTransfer{transfer}); !errors.Is(err, ErrInvalidDebtGroupTransfer) {
			t.Errorf("Apply(%+v) error = %v, want ErrInvalidDebtGroupTransfer", transfer, err)
		}
	}
}
