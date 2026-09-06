// Package arda-money is the single standard for monetary arithmetic across
// Arda backend services. Never compute amounts with float64: all money math
// goes through decimal values plus explicit currency-exponent rounding.
//
// Storage/transport standard (schema-level, see docs/db-schema-conventions):
// monetary amounts cross service boundaries as int64 MINOR units
// (Postgres BIGINT *_minor / proto int64 / JSON number *_minor); NUMERIC
// columns are reserved for decimal-scaled quantities (rate, exchange rate).
// The FromMinor/ToMinor helpers here are the only sanctioned conversions.
package ardamoney

import (
	"fmt"
	"math"

	"github.com/shopspring/decimal"
)

// Exponents per currency (ISO 4217 minor units). Unknown currency → 2.
var currencyExponents = map[string]int32{
	"VND": 0, "JPY": 0, "KRW": 0,
}

// Exponent returns the ISO 4217 minor-unit exponent of currency
// (VND/JPY/KRW → 0; unknown → 2).
func Exponent(currency string) int32 {
	if e, ok := currencyExponents[currency]; ok {
		return e
	}
	return 2
}

// FromMinor converts an int64 minor-unit amount to a decimal in major units
// (e.g. 3541667 VND → 3541667; 770055 USD-cents → 1234.56).
func FromMinor(minor int64, currency string) decimal.Decimal {
	return decimal.NewFromInt(minor).Shift(-Exponent(currency))
}

// ToMinor converts a decimal major-unit amount to int64 minor units,
// rounding to the currency's exponent (half-away-from-zero). Overflow past
// int64 returns an error instead of silently wrapping.
func ToMinor(value decimal.Decimal, currency string) (int64, error) {
	minor := value.Shift(Exponent(currency)).Round(0)
	if !minor.IsInteger() ||
		minor.GreaterThan(decimal.NewFromInt(math.MaxInt64)) ||
		minor.LessThan(decimal.NewFromInt(math.MinInt64)) {
		return 0, fmt.Errorf("arda-money: %s minor amount %s overflows int64", currency, minor)
	}
	return minor.IntPart(), nil
}

// Round returns value rounded to the currency's minor unit (half-away-from-
// zero — the accounting convention), e.g. VND → integer dong, USD → 2 decimals.
func Round(value decimal.Decimal, currency string) decimal.Decimal {
	return value.Round(Exponent(currency))
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
	share := Round(total.Div(decimal.NewFromInt(int64(parts))), currency)

	shares := make([]decimal.Decimal, parts)
	allocated := decimal.Zero
	for i := range shares {
		shares[i] = share
		allocated = allocated.Add(share)
	}
	// Distribute any rounding remainder in minor-unit steps to the first parts.
	diff := total.Sub(allocated)
	one := decimal.NewFromInt(1).Shift(-Exponent(currency))
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

// MustToMinor is ToMinor for schedule/plan math where an overflow is a
// programmer error, not a request error — it panics instead. Request-driven
// conversion must use ToMinor and surface the error to the caller.
func MustToMinor(value decimal.Decimal, currency string) int64 {
	minor, err := ToMinor(value, currency)
	if err != nil {
		panic(err)
	}
	return minor
}
