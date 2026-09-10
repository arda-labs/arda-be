// Package interest ports the EPAS DPM interest calculator
// (be_dpm/core/interest/DpmInterestCalculator): segment the [from, to]
// inclusive range at every balance point, accrue
// balance × days × rate / 100 / denominator (default 365, 360 supported) at
// scale 10, then round to a multiple of roundNo with the configured mode
// (CM163.001 HALF_UP, .002 CEILING, .003 FLOOR).
package interest

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

const (
	RoundHalfUp = "CM163.001"
	RoundUp     = "CM163.002"
	RoundDown   = "CM163.003"

	calcScale              = 10
	defaultBaseDenominator = 365
)

var (
	oneHundred = decimal.NewFromInt(100)
	oneDay     = 24 * time.Hour
)

// BalancePoint is the balance effective from Date onward.
type BalancePoint struct {
	Date    time.Time
	Balance decimal.Decimal
}

// Input is one accrual request.
type Input struct {
	From            time.Time
	To              time.Time
	Rate            decimal.Decimal
	BaseDenominator int
	RoundNo         int
	RoundType       string
	Points          []BalancePoint
}

// Segment is one accrual slice between two balance points.
type Segment struct {
	From    time.Time
	To      time.Time
	Days    int64
	Balance decimal.Decimal
	Amount  decimal.Decimal
}

// Result carries the raw and rounded totals plus the per-segment detail.
type Result struct {
	TotalDays int64
	Raw       decimal.Decimal
	Rounded   decimal.Decimal
	Segments  []Segment
}

// Calculate accrues interest over the inclusive [From, To] range.
func Calculate(in Input) Result {
	if in.To.Before(in.From) {
		return Result{Raw: decimal.Zero, Rounded: decimal.Zero, Segments: []Segment{}}
	}
	exclusiveEnd := in.To.AddDate(0, 0, 1)
	denominator := in.BaseDenominator
	if denominator <= 0 {
		denominator = defaultBaseDenominator
	}

	points := normalizePoints(in.Points)
	boundaries := buildBoundaries(in.From, exclusiveEnd, points)

	raw := decimal.Zero
	segments := make([]Segment, 0, len(boundaries))
	for i := 0; i+1 < len(boundaries); i++ {
		start, end := boundaries[i], boundaries[i+1]
		days := int64(end.Sub(start) / oneDay)
		if days <= 0 {
			continue
		}
		balance := balanceAt(points, start)
		amount := computeInterest(balance, days, in.Rate, denominator)
		raw = raw.Add(amount)
		segments = append(segments, Segment{From: start, To: end, Days: days, Balance: balance, Amount: amount})
	}
	return Result{
		TotalDays: int64(exclusiveEnd.Sub(in.From) / oneDay),
		Raw:       raw,
		Rounded:   ApplyRound(raw, in.RoundNo, in.RoundType),
		Segments:  segments,
	}
}

// ApplyRound rounds amount to a roundNo multiple with the CM163 mode. A
// non-positive roundNo keeps the amount untouched (EPAS semantics).
func ApplyRound(amount decimal.Decimal, roundNo int, roundType string) decimal.Decimal {
	if roundNo <= 0 {
		return amount
	}
	unit := decimal.NewFromInt(int64(roundNo))
	quotient := amount.DivRound(unit, calcScale)
	switch roundType {
	case RoundUp:
		return quotient.Ceil().Mul(unit)
	case RoundDown:
		return quotient.Floor().Mul(unit)
	default:
		return quotient.Round(0).Mul(unit)
	}
}

func computeInterest(balance decimal.Decimal, days int64, rate decimal.Decimal, denominator int) decimal.Decimal {
	if balance.IsZero() || rate.IsZero() || days <= 0 {
		return decimal.Zero
	}
	return balance.
		Mul(decimal.NewFromInt(days)).
		Mul(rate).
		DivRound(oneHundred, calcScale).
		DivRound(decimal.NewFromInt(int64(denominator)), calcScale)
}

func normalizePoints(points []BalancePoint) []BalancePoint {
	out := make([]BalancePoint, len(points))
	copy(out, points)
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

func buildBoundaries(from, exclusiveEnd time.Time, points []BalancePoint) []time.Time {
	boundaries := []time.Time{from}
	for _, p := range points {
		switch {
		case p.Date.After(from) && p.Date.Before(exclusiveEnd):
			boundaries = append(boundaries, p.Date)
		case p.Date.Equal(from):
			// the first point defines the opening balance, not a boundary
		}
	}
	boundaries = append(boundaries, exclusiveEnd)
	return boundaries
}

func balanceAt(points []BalancePoint, date time.Time) decimal.Decimal {
	balance := decimal.Zero
	for _, p := range points {
		if p.Date.After(date) {
			break
		}
		balance = p.Balance
	}
	return balance
}
