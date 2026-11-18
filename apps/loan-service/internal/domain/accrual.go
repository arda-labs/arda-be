package domain

import (
	"fmt"
	"time"

	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	"github.com/shopspring/decimal"
)

// DaysBetween counts whole days from and to, both formatted as YYYY-MM-DD.
func DaysBetween(fromDate, toDate string) (int, error) {
	from, err := time.Parse("2006-01-02", fromDate)
	if err != nil {
		return 0, fmt.Errorf("invalid from_date %q: %w", fromDate, err)
	}
	to, err := time.Parse("2006-01-02", toDate)
	if err != nil {
		return 0, fmt.Errorf("invalid to_date %q: %w", toDate, err)
	}
	return int(to.Sub(from).Hours() / 24), nil
}

// accrualYearBasisDays is the fixed year basis of the legacy accrual path:
// a 30-day month times 12.
const accrualYearBasisDays = 360

// AccruedInterestMinor computes
//
//	outstanding × annual rate % × days / (100 × 360)
//
// (monthly interest prorated over a 30-day month) in exact decimal arithmetic
// and rounds it ONCE, half away from zero, to the currency's minor unit. A
// negative outstanding accrues nothing.
//
// Legacy path: kept only for AccrualService.RunDaily until the product
// configuration (CM124 date method, year-basis code, ROUND_NO/CM163) and the
// balance and rate history are available to the EOD job. It fixes the basis at
// 360 and rounds every run to the minor unit; use CalculateInterest
// (G2-LNM-LAI-01, G2-LNM-LAI-08, T9.1) for anything new.
func AccruedInterestMinor(outstandingMinor int64, annualRatePercent decimal.Decimal, days int, currency string) (int64, error) {
	if currency == "" {
		currency = "VND"
	}
	if outstandingMinor <= 0 || days <= 0 || annualRatePercent.IsZero() {
		return 0, nil
	}
	interest := ardamoney.FromMinor(outstandingMinor, currency).
		Mul(annualRatePercent).
		Mul(decimal.NewFromInt(int64(days))).
		DivRound(decimal.NewFromInt(100*accrualYearBasisDays), ardamoney.Exponent(currency))
	return ardamoney.ToMinor(interest, currency)
}
