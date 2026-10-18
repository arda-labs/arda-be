// Package interest ports the EPAS DPM interest calculator
// (be_dpm/core/interest/DpmInterestCalculator): segment the half-open range
// [From, To) at every balance point and every rate point, accrue
// balance × days × rate / 100 / denominator (default 365, 360 supported) at
// scale 10, sum the raw amounts, then round the TOTAL once to a multiple of
// roundNo with the configured mode (CM163.001 HALF_UP, .002 CEILING, .003 FLOOR).
//
// Day counting is half-open (G2-DPM-NGAY-01 A): the From day accrues interest,
// the To day (the transaction day) does not, so 01/01 → 01/01 of the next year
// is exactly 365 days. Callers holding an inclusive last-accrued day pass
// lastDay + 1 as To.
package interest

import (
	"math/big"
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

// RatePoint is the annual rate (percent) effective from Date onward.
type RatePoint struct {
	Date time.Time
	Rate decimal.Decimal
}

// Input is one accrual request over the half-open range [From, To).
type Input struct {
	From time.Time
	// To is the exclusive end: the To day itself accrues nothing.
	To time.Time
	// Rate is the contract rate used before the first RatePoint.
	Rate            decimal.Decimal
	BaseDenominator int
	RoundNo         int
	RoundType       string
	Points          []BalancePoint
	// RatePoints are the rate changes of the contract; the latest point with
	// Date <= the segment start applies, a later entry winning on equal dates.
	RatePoints []RatePoint
}

// Segment is one accrual slice between two boundaries; To is exclusive.
type Segment struct {
	From    time.Time
	To      time.Time
	Days    int64
	Balance decimal.Decimal
	Rate    decimal.Decimal
	Amount  decimal.Decimal
}

// Result carries the raw and rounded totals plus the per-segment detail.
type Result struct {
	TotalDays int64
	Raw       decimal.Decimal
	Rounded   decimal.Decimal
	Segments  []Segment
}

// Calculate accrues interest over the half-open range [From, To).
func Calculate(in Input) Result {
	if !in.To.After(in.From) {
		return Result{Raw: decimal.Zero, Rounded: decimal.Zero, Segments: []Segment{}}
	}
	denominator := in.BaseDenominator
	if denominator <= 0 {
		denominator = defaultBaseDenominator
	}

	points := normalizePoints(in.Points)
	ratePoints := normalizeRatePoints(in.RatePoints)
	boundaries := buildBoundaries(in.From, in.To, points, ratePoints)

	raw := decimal.Zero
	segments := make([]Segment, 0, len(boundaries))
	for i := 0; i+1 < len(boundaries); i++ {
		start, end := boundaries[i], boundaries[i+1]
		days := int64(end.Sub(start) / oneDay)
		if days <= 0 {
			continue
		}
		balance := balanceAt(points, start)
		rate := rateAt(ratePoints, start, in.Rate)
		amount := computeInterest(balance, days, rate, denominator)
		raw = raw.Add(amount)
		segments = append(segments, Segment{From: start, To: end, Days: days, Balance: balance, Rate: rate, Amount: amount})
	}
	return Result{
		TotalDays: int64(in.To.Sub(in.From) / oneDay),
		Raw:       raw,
		Rounded:   ApplyRound(raw, in.RoundNo, in.RoundType),
		Segments:  segments,
	}
}

// ApplyRound rounds amount to a roundNo multiple with the CM163 mode, exactly
// and in one step (EPAS divide(unit, 0, mode) × unit). A non-positive roundNo
// keeps the amount untouched; an unrecognised mode falls back to HALF_UP, like
// EPAS.
func ApplyRound(amount decimal.Decimal, roundNo int, roundType string) decimal.Decimal {
	if roundNo <= 0 {
		return amount
	}
	quotient := new(big.Rat).Quo(amount.Rat(), new(big.Rat).SetInt64(int64(roundNo)))
	var n *big.Int
	switch roundType {
	case RoundUp:
		n = ceilRat(quotient)
	case RoundDown:
		n = floorRat(quotient)
	default:
		n = halfUpRat(quotient)
	}
	return decimal.NewFromBigInt(n, 0).Mul(decimal.NewFromInt(int64(roundNo)))
}

// floorRat is ⌊x⌋ (Euclidean division by a positive denominator).
func floorRat(x *big.Rat) *big.Int {
	return new(big.Int).Div(x.Num(), x.Denom())
}

func ceilRat(x *big.Rat) *big.Int {
	f := floorRat(x)
	if new(big.Rat).SetInt(f).Cmp(x) == 0 {
		return f
	}
	return f.Add(f, big.NewInt(1))
}

// halfUpRat rounds half away from zero, like BigDecimal HALF_UP.
func halfUpRat(x *big.Rat) *big.Int {
	half := big.NewRat(1, 2)
	if x.Sign() >= 0 {
		return floorRat(new(big.Rat).Add(x, half))
	}
	return new(big.Int).Neg(floorRat(new(big.Rat).Add(new(big.Rat).Neg(x), half)))
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

// normalizePoints sorts by date; the sort is stable so that, on equal dates,
// the point supplied later wins (EPAS sorted(Comparator.comparing(date))).
func normalizePoints(points []BalancePoint) []BalancePoint {
	out := make([]BalancePoint, len(points))
	copy(out, points)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

func normalizeRatePoints(points []RatePoint) []RatePoint {
	out := make([]RatePoint, len(points))
	copy(out, points)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// buildBoundaries returns From, every distinct balance/rate point date inside
// (From, To), then To. Points at or before From shape the opening values only.
func buildBoundaries(from, to time.Time, points []BalancePoint, ratePoints []RatePoint) []time.Time {
	cuts := make([]time.Time, 0, len(points)+len(ratePoints))
	for _, p := range points {
		if p.Date.After(from) && p.Date.Before(to) {
			cuts = append(cuts, p.Date)
		}
	}
	for _, p := range ratePoints {
		if p.Date.After(from) && p.Date.Before(to) {
			cuts = append(cuts, p.Date)
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })
	boundaries := []time.Time{from}
	for _, c := range cuts {
		if c.After(boundaries[len(boundaries)-1]) {
			boundaries = append(boundaries, c)
		}
	}
	return append(boundaries, to)
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

func rateAt(points []RatePoint, date time.Time, fallback decimal.Decimal) decimal.Decimal {
	rate := fallback
	for _, p := range points {
		if p.Date.After(date) {
			break
		}
		rate = p.Rate
	}
	return rate
}
