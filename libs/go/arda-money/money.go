// Package arda-money is the single standard for monetary arithmetic across
// Arda backend services. Never compute amounts with float64: all money math
// goes through decimal values plus explicit currency-exponent rounding.
package ardamoney

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Exponents per currency (ISO 4217 minor units). Unknown currency → 2.
var currencyExponents = map[string]int32{
	"VND": 0, "JPY": 0, "KRW": 0,
}

// Round returns value rounded to the currency's minor unit (half-away-from-
// zero — the accounting convention), e.g. VND → integer dong, USD → 2 decimals.
func Round(value decimal.Decimal, currency string) decimal.Decimal {
	exp := int32(2)
	if e, ok := currencyExponents[currency]; ok {
		exp = e
	}
	return value.Round(exp)
}

// MonthlyInterest computes one month of interest on principal at an annual
// percent rate (e.g. rate=8.5 means 8.5%/year), rounded to the currency
// minor unit: principal * rate / 100 / 12.
func MonthlyInterest(principal decimal.Decimal, annualRatePercent decimal.Decimal, currency string) decimal.Decimal {
	if principal.IsNegative() {
		principal = decimal.Zero
	}
	interest := principal.Mul(annualRatePercent).Div(decimal.NewFromInt(100)).Div(decimal.NewFromInt(12))
	return Round(interest, currency)
}

// AllocateEven splits total into parts of equal shares, assigning the
// remainder (in minor units) to the earliest parts so the sum is exact —
// the only safe way to spread an amount across schedule rows.
func AllocateEven(total decimal.Decimal, parts int, currency string) []decimal.Decimal {
	if parts <= 0 {
		return nil
	}
	exp := int32(2)
	if e, ok := currencyExponents[currency]; ok {
		exp = e
	}
	unit := decimal.NewFromFloat(1).Shift(-exp) // 1 or 0.01
	share := total.Div(decimal.NewFromInt(int64(parts))).Round(exp)

	shares := make([]decimal.Decimal, parts)
	allocated := decimal.Zero
	for i := range shares {
		shares[i] = share
		allocated = allocated.Add(share)
	}
	// Distribute any rounding remainder in unit steps to the first parts.
	diff := total.Sub(allocated)
	one := unit
	i := 0
	for !diff.IsZero() && i < parts {
		switch {
		case diff.GreaterThanOrEqual(one):
			shares[i] = shares[i].Add(one)
			diff = diff.Sub(one)
		case diff.LessThanOrEqual(one.Neg()):
			shares[i] = shares[i].Sub(one)
			diff = diff.Add(one)
		default:
			shares[i] = shares[i].Add(diff)
			diff = decimal.Zero
		}
		i++
	}
	return shares
}

// SubFloor subtracts subtrahend from value, never going below zero — the
// standard "reduce outstanding balance" operation.
func SubFloor(value, subtrahend decimal.Decimal) decimal.Decimal {
	result := value.Sub(subtrahend)
	if result.IsNegative() {
		return decimal.Zero
	}
	return result
}

// MustFromString parses a decimal from a string, panicking on failure — for
// constants and tests only; request parsing must handle errors explicitly.
func MustFromString(raw string) decimal.Decimal {
	value, err := decimal.NewFromString(raw)
	if err != nil {
		panic(fmt.Sprintf("arda-money: invalid decimal %q: %v", raw, err))
	}
	return value
}
