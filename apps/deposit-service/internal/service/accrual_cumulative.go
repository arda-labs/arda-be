package service

import (
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/interest"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/shopspring/decimal"
)

// CodeAccrualRoundConfigMissing is the problem code for a savings whose product
// has no ROUND_NO / CM163 configuration: its daily accrual is skipped and the
// run reports it at the end instead of guessing a rounding step.
const CodeAccrualRoundConfigMissing = "dpm.accrual.round_config_missing"

// topUp is a posted additional deposit dated TxnDate.
type topUp struct {
	Date   time.Time
	Amount decimal.Decimal
}

// cumulativeAccrualInput describes one savings account on one business date for
// the cumulative daily accrual (DPM.305, owner decision of 2026-10-18):
//
//	amount(d) = round(interest accrued from the period start through day d)
//	            − total already accrued in the period
//
// The interest period starts at PeriodStart (the open date, or the effective
// date of the last posted PAY/CAPITALIZE op); every figure that has to be known
// is read from stored data, nothing is kept in memory between runs.
type cumulativeAccrualInput struct {
	PeriodStart  time.Time
	BusinessDate time.Time // inclusive: the run accrues through this day
	// CurrentPrincipal is the stored principal now. The opening balance of the
	// period is this value minus the top-ups dated on or after PeriodStart.
	CurrentPrincipal decimal.Decimal
	TopUps           []topUp
	// Rate is the rate in force on PeriodStart; RatePoints are the later changes.
	Rate        decimal.Decimal
	RatePoints  []interest.RatePoint
	Denominator int
	// RoundNo and RoundType come from the product; nil means "not configured".
	RoundNo   *int
	RoundType *string
	// PriorAccrued is the sum of the amounts already accrued in this period.
	PriorAccrued int64
}

// cumulativeAccrual is the result for one day. Amount may be zero or negative
// (earlier days were rounded up); the caller books only positive amounts.
type cumulativeAccrual struct {
	Raw        decimal.Decimal
	Cumulative int64 // rounded interest of the whole period so far
	Amount     int64 // Cumulative − PriorAccrued
}

// computeCumulativeAccrual is pure. It fails with CodeAccrualRoundConfigMissing
// when the product rounding is not fully configured.
func computeCumulativeAccrual(in cumulativeAccrualInput) (cumulativeAccrual, error) {
	if in.RoundNo == nil || in.RoundType == nil || strings.TrimSpace(*in.RoundType) == "" {
		return cumulativeAccrual{}, ardaerrors.New(CodeAccrualRoundConfigMissing,
			"product has no ROUND_NO/ROUND_TYPE configured for interest accrual")
	}
	if in.BusinessDate.Before(in.PeriodStart) {
		return cumulativeAccrual{Raw: decimal.Zero}, nil
	}
	opening := in.CurrentPrincipal
	for _, t := range in.TopUps {
		if !t.Date.Before(in.PeriodStart) {
			opening = opening.Sub(t.Amount)
		}
	}
	points := []interest.BalancePoint{{Date: in.PeriodStart, Balance: opening}}
	running := opening
	for _, t := range sortedTopUps(in.TopUps) {
		if t.Date.Before(in.PeriodStart) {
			continue
		}
		running = running.Add(t.Amount)
		points = append(points, interest.BalancePoint{Date: t.Date, Balance: running})
	}
	res := interest.Calculate(interest.Input{
		From:            in.PeriodStart,
		To:              in.BusinessDate.AddDate(0, 0, 1),
		Rate:            in.Rate,
		BaseDenominator: in.Denominator,
		RoundNo:         *in.RoundNo,
		RoundType:       *in.RoundType,
		Points:          points,
		RatePoints:      in.RatePoints,
	})
	cumulative := res.Rounded.Round(0).IntPart()
	return cumulativeAccrual{Raw: res.Raw, Cumulative: cumulative, Amount: cumulative - in.PriorAccrued}, nil
}

// sortedTopUps orders top-ups by date, keeping the input order on equal dates.
func sortedTopUps(in []topUp) []topUp {
	out := make([]topUp, len(in))
	copy(out, in)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Date.Before(out[j-1].Date); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// RateOn is the annual rate (percent) in force on date: the latest rate point
// dated on or before it, else the rate of the period start.
func (in cumulativeAccrualInput) RateOn(date time.Time) decimal.Decimal {
	rate := in.Rate
	var at time.Time
	for _, p := range in.RatePoints {
		if !p.Date.After(date) && !p.Date.Before(at) {
			rate, at = p.Rate, p.Date
		}
	}
	return rate
}
