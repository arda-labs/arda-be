package domain

// Loan interest calculator (T9.1). Pure functions, no I/O.
//
// Interest = outstanding × annual rate × actual days / year basis, where the
// year basis comes from configuration (never fixed), the in-term and the overdue
// principal accrue at their own rates, and the day counting follows the CM124
// date-method flag of the product:
//
//	CM124.002  the first period counts the disbursement day AND the last day;
//	           a settlement drops the last day; later periods start at the
//	           milestone + 1.
//	CM124.001  the first accrual day is milestone + 1 and every change dated D
//	           (balance, rate, overdue, principal payment) takes effect on D + 1.
//
// A day on which both in-term and overdue principal are zero accrues nothing,
// so a loan whose disbursement completes late, or whose balance falls to zero
// and comes back, never back-fills those days into a later run
// (G2-LNM-LAI-01 A, G2-LNM-LAI-08 A, post-P3 decision P3-2).
//
// The total is rounded ONCE, to the product ROUND_NO with the CM163 mode, on
// the sum of the in-term and overdue interest of the run (post-P3 decision
// P3-1). EPAS defects are deliberately not copied: CM163.002/.003 codes are
// honoured (EPAS turns them into HALF_UP) and the rounding is a single exact
// division (EPAS roundCurrency divides at scale 10 first).
//
// Out of scope here (no decision yet): the over/under interest adjustment
// (LNM_APPLY_INTEREST_DIFF), receivable deductions and INT_ALLOC.

import (
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// CM124 date-method codes (day counting of a loan product).
const (
	DateMethodSkipFirstDay = "CM124.001"
	DateMethodSkipLastDay  = "CM124.002"
)

// CM163 rounding-mode codes (product ROUND_TYPE). The mode names stored by the
// EPAS front-end (ROUND, CEIL, FLOOR) are accepted as well.
const (
	RoundModeHalfUp  = "CM163.001"
	RoundModeCeiling = "CM163.002"
	RoundModeFloor   = "CM163.003"
)

// Error codes returned by the interest calculator.
const (
	// ErrCodeInterestBaseUnknown: the year-basis code is empty or not in the
	// known table. Never guessed.
	ErrCodeInterestBaseUnknown = "lnm.interest.base_code_unknown"
	// ErrCodeInterestBaseUndefined: CSTL_01 has no confirmed meaning yet
	// (G2-LNM-LAI-01); it must not be used until the business defines it.
	ErrCodeInterestBaseUndefined = "lnm.interest.base_code_undefined"
	// ErrCodeInterestOverRateMissing: overdue principal exists but no overdue
	// rate is configured for the day.
	ErrCodeInterestOverRateMissing = "lnm.interest.over_rate_missing"
	// ErrCodeInterestBalanceMissing: no balance history up to the calculation date.
	ErrCodeInterestBalanceMissing = "lnm.interest.balance_missing"
)

// InterestError is a coded calculator failure.
type InterestError struct {
	Code    string
	Message string
}

func (e *InterestError) Error() string { return e.Code + ": " + e.Message }

// interestBaseDays is the known year-basis table. CSTL_01 is intentionally
// absent (see ErrCodeInterestBaseUndefined).
var interestBaseDays = map[string]int{
	"CSTL_02":  365,
	"CSTL_365": 365,
	"CSTL_360": 360,
	"CSTL_366": 366,
	"365":      365,
	"360":      360,
	"366":      366,
}

// InterestBaseDays resolves a year-basis code to its day count. An empty or
// unrecognised code is an error, not a default.
func InterestBaseDays(code string) (int, error) {
	key := strings.ToUpper(strings.TrimSpace(code))
	if key == "CSTL_01" {
		return 0, &InterestError{Code: ErrCodeInterestBaseUndefined, Message: "year-basis code CSTL_01 has no confirmed definition yet"}
	}
	if days, ok := interestBaseDays[key]; ok {
		return days, nil
	}
	return 0, &InterestError{Code: ErrCodeInterestBaseUnknown, Message: "unknown year-basis code " + quoteCode(code)}
}

func quoteCode(code string) string {
	if code == "" {
		return "(empty)"
	}
	return `"` + code + `"`
}

// InterestFromDay is the first day of a period that accrues interest. base is
// the last accrual/collection milestone, nil when none yet. The first period
// (base not after the disbursement day, or no base) of a CM124.002 product
// starts ON the base (the disbursement day counts); every other case starts at
// base + 1.
func InterestFromDay(disburse time.Time, base *time.Time, dateMethod string) time.Time {
	b := disburse
	if base != nil {
		b = *base
	}
	firstPeriod := civilDay(b) <= civilDay(disburse)
	if firstPeriod && strings.Contains(dateMethod, DateMethodSkipLastDay) {
		return dayTime(civilDay(b))
	}
	return dayTime(civilDay(b) + 1)
}

// RoundInterest rounds amount to a multiple of roundNo with the CM163 mode, in
// one exact step. roundNo <= 0 keeps the amount untouched (EPAS). An empty or
// unrecognised mode is HALF_UP (half away from zero).
func RoundInterest(amount decimal.Decimal, roundNo int, roundType string) decimal.Decimal {
	if roundNo <= 0 {
		return amount
	}
	q := new(big.Rat).Quo(amount.Rat(), new(big.Rat).SetInt64(int64(roundNo)))
	var n *big.Int
	switch {
	case strings.Contains(roundType, "CEIL") || strings.Contains(roundType, RoundModeCeiling):
		n = ratCeil(q)
	case strings.Contains(roundType, "FLOOR") || strings.Contains(roundType, RoundModeFloor):
		n = ratFloor(q)
	default:
		n = ratHalfUp(q)
	}
	return decimal.NewFromBigInt(n, 0).Mul(decimal.NewFromInt(int64(roundNo)))
}

func ratFloor(x *big.Rat) *big.Int { return new(big.Int).Div(x.Num(), x.Denom()) }

func ratCeil(x *big.Rat) *big.Int {
	f := ratFloor(x)
	if new(big.Rat).SetInt(f).Cmp(x) == 0 {
		return f
	}
	return f.Add(f, big.NewInt(1))
}

func ratHalfUp(x *big.Rat) *big.Int {
	half := big.NewRat(1, 2)
	if x.Sign() >= 0 {
		return ratFloor(new(big.Rat).Add(x, half))
	}
	return new(big.Int).Neg(ratFloor(new(big.Rat).Add(new(big.Rat).Neg(x), half)))
}

// BalanceEntry is the total outstanding principal (in-term + overdue) at the
// end of ValueDate, one entry per value date (a later entry wins on equal dates).
type BalanceEntry struct {
	ValueDate time.Time
	Balance   decimal.Decimal
}

// RateEntry is a rate history row effective from EffectiveDate. A nil Rate or
// OverRate means "not configured".
type RateEntry struct {
	EffectiveDate time.Time
	Rate          *decimal.Decimal
	OverRate      *decimal.Decimal
}

// OverdueRule marks the day principal turns overdue: Date + GraceDays, or Date
// itself for an extension.
type OverdueRule struct {
	Date      time.Time
	Extension bool
	GraceDays int
}

// PrincipalPayment is a principal repayment dated Date that is not yet in the
// balance history (it takes the overdue part first).
type PrincipalPayment struct {
	Date   time.Time
	Amount decimal.Decimal
}

// InterestInput is one interest calculation over [From, To] (both days counted
// unless the date method says otherwise). From comes from InterestFromDay.
type InterestInput struct {
	From         time.Time
	To           time.Time
	DateMethod   string // CM124.001 / CM124.002
	IsSettlement bool   // settlement run: CM124.002 drops the last day
	BaseCode     string // year basis code, see InterestBaseDays
	RoundNo      int
	RoundType    string

	Balances []BalanceEntry
	Rates    []RateEntry
	// AgreementRate and AgreementOverRate apply before the first rate-history
	// row (nil when the agreement carries none).
	AgreementRate     *decimal.Decimal
	AgreementOverRate *decimal.Decimal
	Overdue           *OverdueRule
	Payment           *PrincipalPayment
}

// InterestSegment is a run of consecutive days with identical principal and
// rates; To is the last day counted (inclusive).
type InterestSegment struct {
	From             time.Time
	To               time.Time
	Days             int
	NormalPrincipal  decimal.Decimal
	OverduePrincipal decimal.Decimal
	Rate             *decimal.Decimal
	OverRate         *decimal.Decimal
	NormalInterest   decimal.Decimal
	OverdueInterest  decimal.Decimal
}

// InterestResult carries the unrounded in-term and overdue interest, the
// rounded total and the per-segment detail.
type InterestResult struct {
	Normal  decimal.Decimal // unrounded in-term interest
	Overdue decimal.Decimal // unrounded overdue interest
	Raw     decimal.Decimal // Normal + Overdue, unrounded
	Total   decimal.Decimal // Raw rounded once by ROUND_NO / CM163
	// NormalRounded and OverdueRounded are the in-term and overdue totals, each
	// rounded once. A caller that applies the over/under interest adjustment
	// (out of scope here) rounds the sum of these two parts, as EPAS does for an
	// accrual run with LNM_APPLY_INTEREST_DIFF.
	NormalRounded  decimal.Decimal
	OverdueRounded decimal.Decimal
	Days           int // days with outstanding principal
	Segments       []InterestSegment
}

const interestScale = 10

type interestEvent struct {
	eff, prio int
	kind      byte // 'r' rate, 'b' balance, 'o' overdue, 'p' payment
	rate      *decimal.Decimal
	overRate  *decimal.Decimal
	amount    decimal.Decimal
}

type interestState struct {
	normal, overdue decimal.Decimal
	rate, overRate  *decimal.Decimal
}

// CalculateInterest accrues loan interest for one calculation.
func CalculateInterest(in InterestInput) (InterestResult, error) {
	res := zeroInterestResult()
	base, err := InterestBaseDays(in.BaseCode)
	if err != nil {
		return res, err
	}
	first, last := civilDay(in.From), civilDay(in.To)
	skipLast := strings.Contains(in.DateMethod, DateMethodSkipLastDay)
	shift := 0
	if strings.Contains(in.DateMethod, DateMethodSkipFirstDay) {
		shift = 1
	}
	if in.IsSettlement && skipLast {
		last--
	}

	history := normalizeBalances(in.Balances, civilDay(in.To))
	if len(history) == 0 {
		return res, &InterestError{Code: ErrCodeInterestBalanceMissing, Message: "no balance history up to the calculation date"}
	}
	// Balance timeline of the period: the opening balance on the first day,
	// then every later change.
	var timeline []dayBalance
	opening := -1
	for i := range history {
		if history[i].day <= first {
			opening = i
		}
	}
	if opening >= 0 {
		timeline = append(timeline, dayBalance{first, history[opening].balance})
	}
	for _, h := range history {
		if h.day > first {
			timeline = append(timeline, h)
		}
	}

	effective := func(day int) int {
		if day <= first {
			return first
		}
		return day + shift
	}
	events := rateEvents(in, first, civilDay(in.To), effective)
	{
		var prev *decimal.Decimal
		for _, p := range timeline {
			if prev == nil || !p.balance.Equal(*prev) {
				events = append(events, interestEvent{eff: effective(p.day), prio: 1, kind: 'b', amount: p.balance})
				b := p.balance
				prev = &b
			}
		}
	}
	if o := in.Overdue; o != nil {
		start := civilDay(o.Date)
		if !o.Extension && o.GraceDays > 0 {
			start += o.GraceDays
		}
		if start <= civilDay(in.To) {
			day := max(start, first)
			events = append(events, interestEvent{eff: effective(day), prio: 2, kind: 'o', amount: balanceOn(timeline, day)})
		}
	}
	if p := in.Payment; p != nil && p.Amount.IsPositive() && civilDay(p.Date) <= civilDay(in.To) {
		events = append(events, interestEvent{eff: effective(civilDay(p.Date)), prio: 3, kind: 'p', amount: p.Amount})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].eff != events[j].eff {
			return events[i].eff < events[j].eff
		}
		return events[i].prio < events[j].prio
	})

	if last < first {
		return res, nil
	}
	// Boundaries: the first day and every event day inside [first, last].
	cuts := []int{first}
	for _, e := range events {
		if e.eff > first && e.eff <= last && e.eff != cuts[len(cuts)-1] {
			cuts = append(cuts, e.eff)
		}
	}
	state := interestState{normal: decimal.Zero, overdue: decimal.Zero}
	ei := 0
	for i, cut := range cuts {
		for ei < len(events) && events[ei].eff <= cut {
			state = applyInterestEvent(state, events[ei])
			ei++
		}
		end := last
		if i+1 < len(cuts) {
			end = cuts[i+1] - 1
		}
		days := end - cut + 1
		total := state.normal.Add(state.overdue)
		if !total.IsPositive() {
			continue
		}
		seg := InterestSegment{
			From: dayTime(cut), To: dayTime(end), Days: days,
			NormalPrincipal: state.normal, OverduePrincipal: state.overdue,
			Rate: state.rate, OverRate: state.overRate,
			NormalInterest: decimal.Zero, OverdueInterest: decimal.Zero,
		}
		if state.normal.IsPositive() && state.rate != nil {
			seg.NormalInterest = segmentInterest(state.normal, *state.rate, days, base)
		}
		if state.overdue.IsPositive() {
			if state.overRate == nil {
				return zeroInterestResult(), &InterestError{Code: ErrCodeInterestOverRateMissing, Message: "overdue principal without an overdue rate from " + dayTime(cut).Format("2006-01-02")}
			}
			seg.OverdueInterest = segmentInterest(state.overdue, *state.overRate, days, base)
		}
		res.Normal = res.Normal.Add(seg.NormalInterest)
		res.Overdue = res.Overdue.Add(seg.OverdueInterest)
		res.Days += days
		res.Segments = append(res.Segments, seg)
	}
	res.Raw = res.Normal.Add(res.Overdue)
	res.Total = RoundInterest(res.Raw, in.RoundNo, in.RoundType)
	res.NormalRounded = RoundInterest(res.Normal, in.RoundNo, in.RoundType)
	res.OverdueRounded = RoundInterest(res.Overdue, in.RoundNo, in.RoundType)
	return res, nil
}

func zeroInterestResult() InterestResult {
	z := decimal.Zero
	return InterestResult{Normal: z, Overdue: z, Raw: z, Total: z, NormalRounded: z, OverdueRounded: z, Segments: []InterestSegment{}}
}

// segmentInterest = principal × rate% × days / base, one division at scale 10.
func segmentInterest(principal, ratePercent decimal.Decimal, days, base int) decimal.Decimal {
	return principal.Mul(ratePercent).Mul(decimal.NewFromInt(int64(days))).
		DivRound(decimal.NewFromInt(int64(100*base)), interestScale)
}

func applyInterestEvent(s interestState, e interestEvent) interestState {
	switch e.kind {
	case 'r':
		s.rate, s.overRate = e.rate, e.overRate
	case 'b':
		if e.amount.LessThan(s.overdue) {
			s.overdue = e.amount
			s.normal = decimal.Zero
		} else {
			s.normal = e.amount.Sub(s.overdue)
		}
	case 'o':
		current := s.normal.Add(s.overdue)
		a := decimal.Max(e.amount, decimal.Zero)
		a = decimal.Min(a, current)
		s.overdue = a
		s.normal = decimal.Max(current.Sub(a), decimal.Zero)
	case 'p':
		overduePart := decimal.Min(e.amount, s.overdue)
		s.overdue = s.overdue.Sub(overduePart)
		s.normal = decimal.Max(s.normal.Sub(e.amount.Sub(overduePart)), decimal.Zero)
	}
	return s
}

type dayBalance struct {
	day     int
	balance decimal.Decimal
}

// normalizeBalances keeps entries up to the calculation date, ordered by day,
// one per day (the later entry wins).
func normalizeBalances(entries []BalanceEntry, until int) []dayBalance {
	byDay := map[int]decimal.Decimal{}
	for _, e := range entries {
		if d := civilDay(e.ValueDate); d <= until {
			byDay[d] = e.Balance
		}
	}
	out := make([]dayBalance, 0, len(byDay))
	for d, b := range byDay {
		out = append(out, dayBalance{d, b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].day < out[j].day })
	return out
}

// balanceOn is the balance in force on day: the latest entry on or before it,
// else the earliest entry.
func balanceOn(timeline []dayBalance, day int) decimal.Decimal {
	if len(timeline) == 0 {
		return decimal.Zero
	}
	pick := -1
	for i := range timeline {
		if timeline[i].day <= day {
			pick = i
		}
	}
	if pick < 0 {
		pick = 0
	}
	return timeline[pick].balance
}

// rateEvents builds the rate-change events: the rate in force on the first day
// (the earliest row wins on equal dates, the agreement rate when no row applies),
// then every later row, skipping rows that do not change either rate.
func rateEvents(in InterestInput, first, calcDay int, effective func(int) int) []interestEvent {
	type row struct {
		day            int
		rate, overRate *decimal.Decimal
	}
	rows := make([]RateEntry, 0, len(in.Rates))
	for _, r := range in.Rates {
		if civilDay(r.EffectiveDate) <= calcDay {
			rows = append(rows, r)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return civilDay(rows[i].EffectiveDate) < civilDay(rows[j].EffectiveDate)
	})
	var plist []row
	var start *RateEntry
	for i := range rows {
		d := civilDay(rows[i].EffectiveDate)
		if d <= first && (start == nil || d > civilDay(start.EffectiveDate)) {
			start = &rows[i]
		}
	}
	switch {
	case start != nil:
		plist = append(plist, row{first, start.Rate, start.OverRate})
	case in.AgreementRate != nil || in.AgreementOverRate != nil:
		plist = append(plist, row{first, in.AgreementRate, in.AgreementOverRate})
	}
	for _, r := range rows {
		if d := civilDay(r.EffectiveDate); d > first {
			plist = append(plist, row{d, r.Rate, r.OverRate})
		}
	}
	differs := func(a, b *decimal.Decimal) bool {
		if a == nil || b == nil {
			return (a == nil) != (b == nil)
		}
		return !a.Equal(*b)
	}
	var events []interestEvent
	var lastRate, lastOver *decimal.Decimal
	for i, p := range plist {
		if i == 0 || differs(p.rate, lastRate) || differs(p.overRate, lastOver) {
			events = append(events, interestEvent{eff: effective(p.day), prio: 0, kind: 'r', rate: p.rate, overRate: p.overRate})
			lastRate, lastOver = p.rate, p.overRate
		}
	}
	return events
}

// civilDay is the number of days since 1970-01-01 of t's calendar date,
// independent of the time zone and of daylight saving.
func civilDay(t time.Time) int {
	return int(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

func dayTime(n int) time.Time { return time.Unix(int64(n)*86400, 0).UTC() }
