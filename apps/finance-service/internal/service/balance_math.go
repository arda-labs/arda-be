package service

import (
	"fmt"

	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

// balanceMath centralizes the two-phase balance arithmetic (P1b v2,
// docs/epas-survey/disbursement-flow-deep-dive.md — the EPAS
// balAvailable/balActual model). One BalanceRow is the materialized counter
// set for one account+currency; a line contributes its amount to one of the
// four counters depending on the entry lifecycle phase and direction:
//
//	  phase           DEBIT line                 CREDIT line
//	  ─────────────   ─────────────────────────  ─────────────────────────
//	  PENDING hold    reserved_debit +=  a       reserved_credit += a
//	  POSTED move     posted_debit  +=  a       posted_credit  +=  a
//
// Availability is signed and symmetric (EPAS validateBalanceInAccInfo): the
// account's natural-direction value = opening + posted + reserved, re-signed
// so positive = "holds money" (debit-positive for nature D, credit-positive
// for nature C). Any line whose signed delta is negative is an outflow and
// must fit inside the remaining available value; inflow lines are free.

// natureSign is +1 when the direction increases the account's natural
// balance (debit-nature: DEBIT; credit-nature: CREDIT), −1 otherwise.
func natureSign(nature, direction string) int64 {
	increasing := (nature == "C") == (direction == "CREDIT")
	if increasing {
		return 1
	}
	return -1
}

// lineDelta is the signed effect of one journal line on the account's
// natural value: positive = inflow, negative = outflow.
func lineDelta(nature, direction string, amount int64) int64 {
	return natureSign(nature, direction) * amount
}

// naturalSignedPosted re-signs the posted counters into the account's
// natural direction: positive = the account holds value.
func naturalSignedPosted(row repository.BalanceRow, nature string) int64 {
	diff := row.PostedDebitMinor - row.PostedCreditMinor
	if nature == "C" {
		return -diff
	}
	return diff
}

// naturalSignedReserved re-signs the reserved (pending) counters the same
// way — pending inflows raise availability, pending outflows consume it.
func naturalSignedReserved(row repository.BalanceRow, nature string) int64 {
	diff := row.ReservedDebitMinor - row.ReservedCreditMinor
	if nature == "C" {
		return -diff
	}
	return diff
}

// naturalSignedOpening converts an opening-balance side (direction + positive
// amount) into the same natural sign convention; zero when none exists.
func naturalSignedOpening(side repository.OpeningSide, nature string) int64 {
	if side.AmountMinor <= 0 {
		return 0
	}
	diff := int64(0)
	if side.Direction == "DEBIT" {
		diff = side.AmountMinor
	} else {
		diff = -side.AmountMinor
	}
	if nature == "C" {
		return -diff
	}
	return diff
}

// effectiveAvailable is the value a new outflow must fit under:
// opening + posted + reserved, all in natural sign. This is the two-phase
// invariant — pending proposals already consume availability (reserve at
// create), so N pending proposals cannot together overdraft the account.
func effectiveAvailable(row repository.BalanceRow, opening int64, nature string) int64 {
	return opening + naturalSignedPosted(row, nature) + naturalSignedReserved(row, nature)
}

// checkReserve validates one line at reserve (create-pending) time.
func checkReserve(row repository.BalanceRow, opening int64, nature, direction string, amount int64) error {
	delta := lineDelta(nature, direction, amount)
	if delta >= 0 {
		return nil
	}
	if effectiveAvailable(row, opening, nature)+delta < 0 {
		return fmt.Errorf("BAL_AVAILABLE_IS_NOT_ENOUGH:%s", row.Key.AccountCode)
	}
	return nil
}

// checkPostActual validates one line at post time (PENDING → POSTED): the
// actual (opening + posted) value must cover the outflow on its own — money
// may have really left via other posted paths while this entry sat pending
// (EPAS validateActualBalance at complete). Post never changes availability
// (a hold graduates to actual), so this is a belt-and-braces re-check.
func checkPostActual(row repository.BalanceRow, opening int64, nature, direction string, amount int64) error {
	delta := lineDelta(nature, direction, amount)
	if delta >= 0 {
		return nil
	}
	if opening+naturalSignedPosted(row, nature)+delta < 0 {
		return fmt.Errorf("BAL_ACTUAL_IS_NOT_ENOUGH:%s", row.Key.AccountCode)
	}
	return nil
}

// applyReserve adds one line's amount to the reserved counter it belongs to.
func applyReserve(row *repository.BalanceRow, direction string, amount int64) {
	if direction == "DEBIT" {
		row.ReservedDebitMinor += amount
	} else {
		row.ReservedCreditMinor += amount
	}
}

// applyPost moves one line from its reserved counter to the posted counter
// (the hold graduates — availability is unchanged).
func applyPost(row *repository.BalanceRow, direction string, amount int64) {
	if direction == "DEBIT" {
		row.ReservedDebitMinor -= amount
		row.PostedDebitMinor += amount
	} else {
		row.ReservedCreditMinor -= amount
		row.PostedCreditMinor += amount
	}
}

// applyRelease removes one line's amount from its reserved counter (reject /
// void path — nothing was ever posted).
func applyRelease(row *repository.BalanceRow, direction string, amount int64) {
	if direction == "DEBIT" {
		row.ReservedDebitMinor -= amount
	} else {
		row.ReservedCreditMinor -= amount
	}
}

// applyDirectPost books one line straight to the posted counter (no hold —
// the accrual/provision/cash/deposit/capital direct-post path).
func applyDirectPost(row *repository.BalanceRow, direction string, amount int64) {
	if direction == "DEBIT" {
		row.PostedDebitMinor += amount
	} else {
		row.PostedCreditMinor += amount
	}
}
