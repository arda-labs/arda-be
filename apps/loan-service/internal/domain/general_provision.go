package domain

import "github.com/shopspring/decimal"

// RequiredGeneralProvision is outstanding × rate / 100, rounded HALF_UP to
// the nearest minor currency unit.
func RequiredGeneralProvision(outstandingMinor int64, ratePercent float64) int64 {
	if outstandingMinor <= 0 || ratePercent <= 0 {
		return 0
	}
	required := decimal.NewFromInt(outstandingMinor).
		Mul(decimal.NewFromFloat(ratePercent)).
		Div(decimal.NewFromInt(100)).
		Round(0)
	return required.IntPart()
}

// GeneralProvisionDelta returns the additional allocation or reversal needed
// to make accumulated provision equal the required amount.
func GeneralProvisionDelta(required, accumulated int64) (alloc, reverse int64) {
	switch {
	case required > accumulated:
		return required - accumulated, 0
	case accumulated > required:
		return 0, accumulated - required
	default:
		return 0, 0
	}
}
