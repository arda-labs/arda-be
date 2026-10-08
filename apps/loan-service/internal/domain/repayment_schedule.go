package domain

import (
	"fmt"
	"time"

	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	"github.com/shopspring/decimal"
)

// BuildEvenPrincipalPlans builds a declining-balance schedule whose principal
// shares sum exactly to the agreement's outstanding balance.
func BuildEvenPrincipalPlans(agreement Agreement, termCount int, startDate string) ([]RepayPlan, error) {
	if termCount <= 0 {
		return nil, fmt.Errorf("term count must be positive, got %d", termCount)
	}
	currency := agreement.CurrencyCode
	if currency == "" {
		currency = "VND"
	}
	outstanding := ardamoney.FromMinor(agreement.OutstandingAmt, currency)
	rate := decimal.NewFromFloat(agreement.InterestRate)
	shares := ardamoney.AllocateEven(outstanding, termCount, currency)

	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return nil, fmt.Errorf("invalid effective start date %q: %w", startDate, err)
	}
	plans := make([]RepayPlan, 0, termCount)
	remaining := outstanding
	for i := 1; i <= termCount; i++ {
		principal := shares[i-1]
		from := start.AddDate(0, i-1, 0)
		to := start.AddDate(0, i, 0)
		plans = append(plans, RepayPlan{
			ContractCode:     agreement.ContractCode,
			AgreementCode:    agreement.AgreementCode,
			PlanNo:           1,
			TermNo:           i,
			FromDate:         from.Format("2006-01-02"),
			ToDate:           to.Format("2006-01-02"),
			InterestRate:     agreement.InterestRate,
			PlanPrincipalAmt: ardamoney.MustToMinor(principal, currency),
			PlanInterestAmt:  ardamoney.MustToMinor(ardamoney.MonthlyInterest(remaining, rate, currency), currency),
		})
		remaining = remaining.Sub(principal)
	}
	return plans, nil
}
