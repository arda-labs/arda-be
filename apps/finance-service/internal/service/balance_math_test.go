package service

import (
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

func balanceRow(postedDebit, postedCredit, reservedDebit, reservedCredit int64) repository.BalanceRow {
	return repository.BalanceRow{
		Key:                 repository.BalanceKey{CoaVersion: "V1", AccountCode: "1131", CurrencyCode: "VND"},
		PostedDebitMinor:    postedDebit,
		PostedCreditMinor:   postedCredit,
		ReservedDebitMinor:  reservedDebit,
		ReservedCreditMinor: reservedCredit,
	}
}

// Reserve path: an asset (nature D) account with opening 1_000 and no
// pending holds allows a 500 credit outflow, then rejects a second 600 —
// pending proposals consume availability exactly like EPAS balAvailable.
func TestCheckReserveDebitNatureOutflow(t *testing.T) {
	row := balanceRow(0, 0, 0, 0) // posted empty — value comes from opening
	opening := int64(1_000)

	if err := checkReserve(row, opening, "D", "CREDIT", 500); err != nil {
		t.Fatalf("first reserve must pass: %v", err)
	}
	applyReserve(&row, "CREDIT", 500)
	if err := checkReserve(row, opening, "D", "CREDIT", 600); err == nil ||
		!strings.HasPrefix(err.Error(), "BAL_AVAILABLE_IS_NOT_ENOUGH") {
		t.Fatalf("second reserve must fail with BAL_AVAILABLE_IS_NOT_ENOUGH, got %v", err)
	}
	// After the first hold, only 500 remains available.
	if err := checkReserve(row, opening, "D", "CREDIT", 500); err != nil {
		t.Fatalf("exact-fit reserve must pass: %v", err)
	}
}

// Inflow lines never consume availability.
func TestCheckReserveInflowFree(t *testing.T) {
	row := balanceRow(0, 0, 0, 0)
	if err := checkReserve(row, 0, "D", "DEBIT", 9_999); err != nil {
		t.Fatalf("inflow on debit-nature account must be free: %v", err)
	}
	if err := checkReserve(row, 0, "C", "CREDIT", 9_999); err != nil {
		t.Fatalf("inflow on credit-nature account must be free: %v", err)
	}
}

// Symmetry: a credit-nature (liability) account spends via DEBIT.
func TestCheckReserveCreditNatureOutflow(t *testing.T) {
	row := balanceRow(0, 700, 0, 0) // posted credit 700 = liability holds 700
	if err := checkReserve(row, 0, "C", "DEBIT", 700); err != nil {
		t.Fatalf("exact outflow must pass: %v", err)
	}
	if err := checkReserve(row, 0, "C", "DEBIT", 701); err == nil {
		t.Fatal("overdrafting a liability must fail")
	}
}

// Post path: holds graduate to posted (availability unchanged) and the
// actual check catches money that really left via other posted paths.
func TestCheckPostActualAfterExternalDrain(t *testing.T) {
	// Available at reserve time was fine (opening 1_000, hold 800).
	row := balanceRow(0, 0, 0, 800)
	opening := int64(1_000)
	// sanity: the fresh reserve itself must be possible
	fresh := balanceRow(0, 0, 0, 0)
	if err := checkReserve(fresh, opening, "D", "CREDIT", 800); err != nil {
		t.Fatalf("reserve precondition must hold: %v", err)
	}

	// Meanwhile a direct post drained the account to 100 actual.
	row.PostedCreditMinor += 900
	// Posting the pending hold must now fail the actual check.
	if err := checkPostActual(row, opening, "D", "CREDIT", 800); err == nil ||
		!strings.HasPrefix(err.Error(), "BAL_ACTUAL_IS_NOT_ENOUGH") {
		t.Fatalf("post must fail with BAL_ACTUAL_IS_NOT_ENOUGH, got %v", err)
	}

	// With sufficient funds the move graduates the hold without changing
	// availability.
	row2 := balanceRow(0, 0, 0, 800)
	applyPost(&row2, "CREDIT", 800)
	before := effectiveAvailable(balanceRow(0, 0, 0, 800), opening, "D")
	after := effectiveAvailable(row2, opening, "D")
	if before != after {
		t.Fatalf("post must preserve availability: %d vs %d", before, after)
	}
	if row2.PostedCreditMinor != 800 || row2.ReservedCreditMinor != 0 {
		t.Fatalf("counters must graduate: %+v", row2)
	}
}

// Release frees exactly what the hold took.
func TestApplyReleaseRestoresAvailability(t *testing.T) {
	row := balanceRow(0, 0, 0, 0)
	applyReserve(&row, "CREDIT", 300)
	if got := effectiveAvailable(row, 500, "D"); got != 200 {
		t.Fatalf("available after hold = %d, want 200", got)
	}
	applyRelease(&row, "CREDIT", 300)
	if got := effectiveAvailable(row, 500, "D"); got != 500 {
		t.Fatalf("available after release = %d, want 500", got)
	}
}

// Opening balances carry sign through the natural-direction conversion.
func TestNaturalSignedOpening(t *testing.T) {
	if got := naturalSignedOpening(repository.OpeningSide{Direction: "DEBIT", AmountMinor: 100}, "D"); got != 100 {
		t.Fatalf("debit opening on D account = %d, want 100", got)
	}
	if got := naturalSignedOpening(repository.OpeningSide{Direction: "CREDIT", AmountMinor: 100}, "D"); got != -100 {
		t.Fatalf("credit opening on D account = %d, want -100", got)
	}
	if got := naturalSignedOpening(repository.OpeningSide{Direction: "CREDIT", AmountMinor: 100}, "C"); got != 100 {
		t.Fatalf("credit opening on C account = %d, want 100", got)
	}
}
