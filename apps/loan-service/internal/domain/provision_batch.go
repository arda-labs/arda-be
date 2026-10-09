package domain

import (
	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	"github.com/shopspring/decimal"
)

var debtGroupProvisionRates = map[string]decimal.Decimal{
	"GROUP_1": decimal.Zero,
	"GROUP_2": decimal.NewFromInt(5),
	"GROUP_3": decimal.NewFromInt(20),
	"GROUP_4": decimal.NewFromInt(50),
	"GROUP_5": decimal.NewFromInt(100),
}

// DebtGroupProvisionRate returns the configured batch rate for a debt group.
func DebtGroupProvisionRate(code string) (decimal.Decimal, bool) {
	rate, ok := debtGroupProvisionRates[code]
	return rate, ok
}

// RequiredProvisionMinor computes outstanding × rate / 100 with half-up
// rounding in currency minor units.
func RequiredProvisionMinor(outstandingMinor int64, rate decimal.Decimal, currency string) (int64, error) {
	if currency == "" {
		currency = "VND"
	}
	amount := ardamoney.FromMinor(outstandingMinor, currency).Mul(rate).Div(decimal.NewFromInt(100)).Round(0)
	return ardamoney.ToMinor(amount, currency)
}

// ProvisionDelta is the signed amount needed to bring accumulated provision
// to the required balance. Negative values represent reversals.
func ProvisionDelta(required, accumulated int64) int64 { return required - accumulated }
