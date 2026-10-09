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

// AccruedInterestMinor computes monthly interest prorated over a 30-day month
// and converts it to the currency's minor unit using arda-money rounding.
func AccruedInterestMinor(outstandingMinor int64, ratePercent float64, days int, currency string) (int64, error) {
	if currency == "" {
		currency = "VND"
	}
	proratedRate := decimal.NewFromFloat(ratePercent * float64(days) / 30.0)
	interest := ardamoney.MonthlyInterest(ardamoney.FromMinor(outstandingMinor, currency), proratedRate, currency)
	return ardamoney.ToMinor(interest, currency)
}
