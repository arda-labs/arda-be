package domain

import "github.com/shopspring/decimal"

// RequiredSpecificProvision is base × rate / 100, rounded HALF_UP to the
// nearest minor currency unit.
func RequiredSpecificProvision(baseMinor int64, ratePercent float64) int64 {
	if baseMinor <= 0 || ratePercent <= 0 {
		return 0
	}
	required := decimal.NewFromInt(baseMinor).
		Mul(decimal.NewFromFloat(ratePercent)).
		Div(decimal.NewFromInt(100)).
		Round(0)
	return required.IntPart()
}

// SpecificProvisionDelta returns the positive ADD and REVERSAL journal values.
func SpecificProvisionDelta(required, current int64) (add, reversal int64) {
	switch {
	case required > current:
		return required - current, 0
	case current > required:
		return 0, current - required
	default:
		return 0, 0
	}
}
