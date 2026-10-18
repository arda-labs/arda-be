//go:build epasgolden

// Differential test bộ tính lãi khoản vay: phía Arda (P3-LNM, đợt rà soát 2026-10).
//
// Tệp này nằm cạnh domain/accrual.go của loan-service để gọi NGUYÊN các hàm Arda
// (DaysBetween, AccruedInterestMinor) và arda-money. Không sửa mã sản xuất.
//
//	EPAS_DIFF_IN=vectors.jsonl EPAS_DIFF_OUT=arda-results.jsonl \
//	  go test -tags epasgolden -run TestEpasDiffRun ./apps/loan-service/internal/domain
//
// Gồm ba phần:
//  1. ardaSim: mô phỏng đúng vòng lặp AccrualService.RunDaily (service/accrual_service.go) với
//     kho dữ liệu giả, mỗi ngày một lần chạy EOD, gọi hàm thật domain.AccruedInterestMinor.
//  2. specRun: bộ tính "mục tiêu" theo các quyết định G2 (lãi = dư nợ x lãi suất x số ngày thực tế / cơ sở năm,
//     Decimal, tách quá hạn, đếm ngày theo cờ CM124) viết lại từ ngữ nghĩa, dùng làm thước phân rã nguyên nhân.
//  3. TestEpasGoldenFixture: so khớp fixture oracle (testdata) với Arda hiện tại.
package domain

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	ardamoney "github.com/arda-labs/arda/libs/go/arda-money"
	"github.com/shopspring/decimal"
)

// ------------------------------------------------------------------- kiểu vector

type egBal struct {
	ValueDate string  `json:"value_date"`
	BalActual string  `json:"bal_actual"`
	DataDate  *string `json:"data_date"`
	Bal       *string `json:"bal"`
}

type egRate struct {
	EffectiveDate string  `json:"effective_date"`
	Rate          *string `json:"rate"`
	OverRate      *string `json:"over_rate"`
}

type egCase struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Group    string   `json:"group"`
	Tags     []string `json:"tags"`
	CalcType string   `json:"calc_type"`
	Product  struct {
		BaseCode         *string `json:"base_code"`
		CalcMethod       *string `json:"calc_method"`
		DateMethod       *string `json:"date_method"`
		StartMethod      *string `json:"start_method"`
		OverdueCheck     *string `json:"overdue_check"`
		PrincipalGrace   *int    `json:"principal_grace"`
		InterestGrace    *int    `json:"interest_grace"`
		InterestDiffMgmt *int    `json:"interest_diff_mgmt"`
		RoundNo          *int    `json:"round_no"`
		RoundType        *string `json:"round_type"`
		Currency         string  `json:"currency"`
	} `json:"product"`
	Param struct {
		ApplyInterestDiff *string `json:"apply_interest_diff"`
	} `json:"param"`
	Agreement struct {
		DisburseDate           string  `json:"disburse_date"`
		InterestRate           *string `json:"interest_rate"`
		OverInterestRate       *string `json:"over_interest_rate"`
		DebtGroup              *string `json:"debt_group"`
		InterestToDate         *string `json:"interest_to_date"`
		InterestAllocToDate    *string `json:"interest_alloc_to_date"`
		AcrInterestDate        *string `json:"acr_interest_date"`
		OverdueDate            *string `json:"overdue_date"`
		ExtensionDate          *string `json:"extension_date"`
		InterestDiffAmt        *string `json:"interest_diff_amt"`
		ColnInterestAmt        *string `json:"coln_interest_amt"`
		AcrInterestAmt         *string `json:"acr_interest_amt"`
		OffBalDueInterestAmt   *string `json:"off_bal_due_interest_amt"`
		AcrInterestPeriodAmt   *string `json:"acr_interest_period_amt"`
		AcrInterestPrevYearAmt *string `json:"acr_interest_prev_year_amt"`
	} `json:"agreement"`
	Balances []egBal  `json:"balances"`
	Rates    []egRate `json:"rates"`
	Calc     struct {
		FromDate         *string `json:"from_date"`
		CalculateDate    string  `json:"calculate_date"`
		IsSettlement     bool    `json:"is_settlement"`
		PrincipalPayment *string `json:"principal_payment"`
		PaymentDate      *string `json:"payment_date"`
		AllocFromDate    *string `json:"alloc_from_date"`
	} `json:"calc"`
	// type=round | fe_alloc
	Amount      string  `json:"amount"`
	RoundNo     *int    `json:"round_no"`
	RoundType   *string `json:"round_type"`
	Interest    string  `json:"interest"`
	RateValue   string  `json:"rate_value"`
	PayoutRatio string  `json:"payout_ratio"`
}

// ------------------------------------------------------------------- tiện ích ngày / số

func dayNum(s string) int {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(fmt.Sprintf("ngày không hợp lệ %q: %v", s, err))
	}
	return int(t.Unix() / 86400)
}

func dayStr(n int) string {
	return time.Unix(int64(n)*86400, 0).UTC().Format("2006-01-02")
}

func optDay(s *string) *int {
	if s == nil || *s == "" {
		return nil
	}
	n := dayNum(*s)
	return &n
}

func rat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("số không hợp lệ: " + s)
	}
	return r
}

func optRat(s *string) *big.Rat {
	if s == nil || *s == "" {
		return nil
	}
	return rat(*s)
}

func zeroRat() *big.Rat { return new(big.Rat) }

func ratStr(r *big.Rat) string {
	if r == nil {
		return ""
	}
	if r.IsInt() {
		return r.Num().String()
	}
	return r.FloatString(10)
}

func contains(s *string, sub string) bool {
	return s != nil && strings.Contains(*s, sub)
}

func pos(r *big.Rat) bool { return r != nil && r.Sign() > 0 }

// ------------------------------------------------------------------- phía Arda hiện tại: mô phỏng RunDaily

type mergedBal struct {
	day int
	bal *big.Rat
}

// mergeBalances gộp theo value_date, lấy các dòng có data_date lớn nhất rồi cộng (cùng cách ghép ngữ nghĩa sổ cái).
func mergeBalances(rows []egBal, until int) []mergedBal {
	type acc struct {
		maxData string
		sum     *big.Rat
		hasData bool
	}
	by := map[int][]egBal{}
	for _, r := range rows {
		d := dayNum(r.ValueDate)
		if d > until {
			continue
		}
		by[d] = append(by[d], r)
	}
	days := make([]int, 0, len(by))
	for d := range by {
		days = append(days, d)
	}
	sort.Ints(days)
	out := make([]mergedBal, 0, len(days))
	for _, d := range days {
		recs := by[d]
		maxData := ""
		for _, r := range recs {
			dd := r.ValueDate
			if r.DataDate != nil {
				dd = *r.DataDate
			}
			if dd > maxData {
				maxData = dd
			}
		}
		sum := zeroRat()
		for _, r := range recs {
			dd := r.ValueDate
			if r.DataDate != nil {
				dd = *r.DataDate
			}
			if dd == maxData {
				sum.Add(sum, rat(r.BalActual))
			}
		}
		out = append(out, mergedBal{d, sum})
	}
	return out
}

// ardaOutstanding: dư nợ cuối ngày d theo sổ cái; khoản trả gốc giả định (principal_payment) áp từ ngày trả cho tới khi
// có dòng sổ cái mới hơn (sổ cái là nguồn đúng, cùng ngữ nghĩa EPAS).
func ardaOutstanding(c *egCase, rows []mergedBal, d, windowStart int) *big.Rat {
	var last *mergedBal
	for i := range rows {
		if rows[i].day <= d {
			last = &rows[i]
		} else {
			break
		}
	}
	if last == nil {
		return zeroRat()
	}
	v := new(big.Rat).Set(last.bal)
	pay := optRat(c.Calc.PrincipalPayment)
	pd := optDay(c.Calc.PaymentDate)
	if pay != nil && pay.Sign() > 0 && pd != nil {
		// khoản trả có hiệu lực từ max(ngày trả, đầu kỳ tính); dòng sổ cái mới hơn ngày hiệu lực thì sổ cái là nguồn đúng
		eff := *pd
		if eff < windowStart {
			eff = windowStart
		}
		if d < eff || last.day > eff {
			return v
		}
		v.Sub(v, pay)
		if v.Sign() < 0 {
			v = zeroRat()
		}
	}
	return v
}

func ardaRate(c *egCase, d int) float64 {
	best := -1
	var bestRate *string
	for _, r := range c.Rates {
		e := dayNum(r.EffectiveDate)
		if e <= d && e >= best {
			best = e
			bestRate = r.Rate
		}
	}
	if best < 0 {
		bestRate = c.Agreement.InterestRate
	}
	if bestRate == nil {
		return 0
	}
	f, _ := strconv.ParseFloat(*bestRate, 64)
	return f
}

func ardaRateRat(c *egCase, d int) *big.Rat {
	best := -1
	var bestRate *string
	for _, r := range c.Rates {
		e := dayNum(r.EffectiveDate)
		if e <= d && e >= best {
			best = e
			bestRate = r.Rate
		}
	}
	if best < 0 {
		bestRate = c.Agreement.InterestRate
	}
	if bestRate == nil {
		return zeroRat()
	}
	return rat(*bestRate)
}

// ardaLastAccrual: ngày tính lãi gần nhất của Arda (lnm_accruals.to_date POSTED) tương ứng với mốc EPAS dùng làm đáy kỳ.
func ardaLastAccrual(c *egCase) (*int, string) {
	I := optDay(c.Agreement.InterestToDate)
	A := optDay(c.Agreement.AcrInterestDate)
	switch c.CalcType {
	case "INT_ACCR":
		if contains(c.Product.StartMethod, "CM125.002") {
			return nil, ""
		}
		if c.Param.ApplyInterestDiff != nil && *c.Param.ApplyInterestDiff == "1" {
			if I == nil {
				return nil, "thiếu INTEREST_TO_DATE"
			}
			return I, ""
		}
		if I == nil && A == nil {
			return nil, "thiếu INTEREST_TO_DATE/ACR_INTEREST_DATE"
		}
		if I == nil {
			return A, ""
		}
		if A == nil || *A < *I {
			return I, ""
		}
		return A, ""
	case "INT_ALLOC":
		return optDay(c.Agreement.InterestAllocToDate), ""
	default:
		if contains(c.Product.StartMethod, "CM125.001") && I != nil {
			return I, ""
		}
		return nil, ""
	}
}

type simOut struct {
	Minor     int64
	DecMinor  int64    // cùng vòng lặp, rate*days/30 bằng Decimal chính xác (loại ảnh hưởng float64)
	ExactRuns *big.Rat // tổng chính xác các lần chạy đã ghi sổ
	ExactAll  *big.Rat // tổng chính xác mọi ngày, không bỏ lần chạy nào (không làm tròn)
	Runs      int
	Skipped   int
	Days      int
}

// ardaSim chạy đúng điều kiện và công thức của AccrualService.RunDaily cho từng ngày d trong (L, T]:
//
//	ListActiveAgreementsForAccrual: dư nợ > 0 và disburse_date <= d
//	from = lần tính lãi gần nhất, nếu chưa có thì disburse_date; days = DaysBetween(from, d); days <= 0 thì bỏ qua
//	interest = domain.AccruedInterestMinor(dư nợ, lãi suất, days, "VND"); = 0 thì bỏ qua (không ghi dòng, from giữ nguyên)
func ardaSim(c *egCase, L *int, D, T int) simOut {
	rows := mergeBalances(c.Balances, T)
	out := simOut{ExactRuns: zeroRat(), ExactAll: zeroRat()}
	var last *int = L
	start := D
	if L != nil {
		start = *L + 1
	}
	prevAll := L
	windowStart := start // ngày đầu tiên được tính lãi: sau lần tính gần nhất, hoặc ngày sau giải ngân
	if L == nil {
		windowStart = D + 1
	}
	for d := start; d <= T; d++ {
		o := ardaOutstanding(c, rows, d, windowStart)
		if o.Sign() <= 0 || d < D {
			continue
		}
		// bản "không bỏ qua" để kiểm chứng ngữ nghĩa ngày (độc lập với làm tròn)
		fromAll := D
		if prevAll != nil {
			fromAll = *prevAll
		}
		if dd := d - fromAll; dd > 0 {
			ex := new(big.Rat).Mul(o, ardaRateRat(c, d))
			ex.Mul(ex, big.NewRat(int64(dd), 36000))
			out.ExactAll.Add(out.ExactAll, ex)
			prevAll = &d
		}

		from := D
		if last != nil {
			from = *last
		}
		days, err := DaysBetween(dayStr(from), dayStr(d))
		if err != nil || days <= 0 {
			out.Skipped++
			continue
		}
		o64 := o.Num().Int64() // dư nợ là số nguyên đồng
		minor, err := AccruedInterestMinor(o64, ardaRate(c, d), days, "VND")
		if err != nil || minor == 0 {
			out.Skipped++
			continue
		}
		out.Minor += minor
		out.Runs++
		out.Days += days
		dm, _ := decimalRunMinor(o64, ardaRateRat(c, d), days)
		out.DecMinor += dm
		ex := new(big.Rat).Mul(o, ardaRateRat(c, d))
		ex.Mul(ex, big.NewRat(int64(days), 36000))
		out.ExactRuns.Add(out.ExactRuns, ex)
		x := d
		last = &x
	}
	return out
}

// decimalRunMinor: cùng công thức Arda nhưng lãi suất kỳ = rate*days/30 tính Decimal chính xác (không qua float64).
func decimalRunMinor(outstandingMinor int64, rate *big.Rat, days int) (int64, error) {
	prorated := decimal.NewFromBigRat(new(big.Rat).Mul(rate, big.NewRat(int64(days), 30)), 30)
	interest := ardamoney.MonthlyInterest(ardamoney.FromMinor(outstandingMinor, "VND"), prorated, "VND")
	return ardamoney.ToMinor(interest, "VND")
}

// ------------------------------------------------------------------- bộ tính mục tiêu (thước phân rã)

type rules struct {
	firstDay bool // kỳ đầu tính cả ngày giải ngân (CM124.002); Arda: không
	settle   bool // tất toán bỏ ngày cuối (CM124.002); Arda: không có khái niệm
	lateFill bool // Arda gom ngày trước khi hoàn tất giải ngân vào lần chạy đầu
	gapFill  bool // Arda gom ngày dư nợ 0 giữa kỳ vào lần chạy sau
	shift    bool // CM124.001: thay đổi trong ngày D có hiệu lực từ D+1; Arda: không có cờ
	base360  bool // Arda cố định 30/360
	ovdSplit bool // tách lãi quá hạn theo lãi suất quá hạn; Arda: không
}

func epasRules(c *egCase) rules {
	is001 := contains(c.Product.DateMethod, "CM124.001")
	is002 := contains(c.Product.DateMethod, "CM124.002")
	return rules{firstDay: is002, settle: is002 && c.Calc.IsSettlement, shift: is001, ovdSplit: true}
}

func ardaRules() rules {
	return rules{lateFill: true, gapFill: true, base360: true}
}

func epasBaseDays(code *string) int {
	if code == nil {
		return 365
	}
	switch {
	case strings.Contains(*code, "360"):
		return 360
	case strings.Contains(*code, "366"):
		return 366
	}
	return 365
}

type specEvent struct {
	eff, prio int
	kind      string
	rate      *big.Rat
	orate     *big.Rat
	hasRate   bool
	total     *big.Rat
	amount    *big.Rat
}

type specOut struct {
	Normal, Overdue *big.Rat
	Err             string
	First, Last     int
	Days            int
}

type dayState struct {
	pn, po *big.Rat
	r, ro  *big.Rat // nil = chưa có / thiếu
}

func specRun(c *egCase, rl rules) specOut {
	res := specOut{Normal: zeroRat(), Overdue: zeroRat()}
	D := dayNum(c.Agreement.DisburseDate)
	T := dayNum(c.Calc.CalculateDate)
	I := optDay(c.Agreement.InterestToDate)
	A := optDay(c.Agreement.AcrInterestDate)
	IA := optDay(c.Agreement.InterestAllocToDate)
	applyDiff := c.Param.ApplyInterestDiff != nil && *c.Param.ApplyInterestDiff == "1"

	resolve := func(base int) int {
		if rl.firstDay && base <= D {
			return base
		}
		return base + 1
	}
	var F int
	if fo := optDay(c.Calc.FromDate); fo != nil {
		F = *fo
	} else {
		switch c.CalcType {
		case "INT_ALLOC":
			if IA != nil {
				F = *IA + 1
			} else {
				F = resolve(D)
			}
		case "INT_ACCR":
			if contains(c.Product.StartMethod, "CM125.002") {
				F = resolve(D)
			} else {
				var base int
				if applyDiff {
					if I == nil {
						res.Err = "VALIDATION_ERROR"
						return res
					}
					base = *I
				} else {
					switch {
					case I == nil && A == nil:
						res.Err = "VALIDATION_ERROR"
						return res
					case I == nil:
						base = *A
					case A == nil || *A < *I:
						base = *I
					default:
						base = *A
					}
				}
				F = resolve(base)
			}
		default:
			F = resolve(D)
			if contains(c.Product.StartMethod, "CM125.001") && I != nil {
				F = resolve(*I)
			}
		}
	}
	res.First = F
	Tend := T
	if rl.settle {
		Tend = T - 1
	}
	res.Last = Tend

	// lịch sử số dư nguồn (tới ngày tính)
	src := mergeBalances(c.Balances, T)
	if len(src) == 0 {
		res.Err = "BUSINESS_DATA_MISSING"
		return res
	}
	// danh sách đã xử lý (bản "ctx.balanceHistory")
	var P []mergedBal
	if contains(c.Product.CalcMethod, "CM123.002") {
		for _, b := range src {
			if b.day >= D && b.bal.Sign() > 0 {
				P = []mergedBal{{F, b.bal}}
				break
			}
		}
	} else {
		var startBal *mergedBal
		for i := range src {
			if src[i].day <= F {
				startBal = &src[i]
			}
		}
		if startBal != nil {
			P = append(P, mergedBal{F, startBal.bal})
		}
		for _, b := range src {
			if b.day > F {
				P = append(P, b)
			}
		}
	}

	shift := 0
	if rl.shift {
		shift = 1
	}
	effOf := func(e int) int {
		if e <= F {
			return F
		}
		return e + shift
	}
	var events []specEvent

	// lãi suất
	{
		var rrows []egRate
		for _, r := range c.Rates {
			if dayNum(r.EffectiveDate) <= T {
				rrows = append(rrows, r)
			}
		}
		sort.SliceStable(rrows, func(i, j int) bool { return dayNum(rrows[i].EffectiveDate) < dayNum(rrows[j].EffectiveDate) })
		type rr struct {
			day   int
			r, ro *big.Rat
		}
		var plist []rr
		var startRate *egRate
		for i := range rrows {
			// Stream.max của EPAS giữ phần tử đầu khi trùng ngày hiệu lực
			if dayNum(rrows[i].EffectiveDate) <= F && (startRate == nil || dayNum(rrows[i].EffectiveDate) > dayNum(startRate.EffectiveDate)) {
				startRate = &rrows[i]
			}
		}
		if startRate != nil {
			plist = append(plist, rr{F, optRat(startRate.Rate), optRat(startRate.OverRate)})
		} else if c.Agreement.InterestRate != nil || c.Agreement.OverInterestRate != nil {
			plist = append(plist, rr{F, optRat(c.Agreement.InterestRate), optRat(c.Agreement.OverInterestRate)})
		}
		for _, r := range rrows {
			if dayNum(r.EffectiveDate) > F {
				plist = append(plist, rr{dayNum(r.EffectiveDate), optRat(r.Rate), optRat(r.OverRate)})
			}
		}
		var lr, lro *big.Rat
		first := true
		diff := func(a, b *big.Rat) bool {
			if a == nil || b == nil {
				return (a == nil) != (b == nil)
			}
			return a.Cmp(b) != 0
		}
		for _, p := range plist {
			if first || diff(p.r, lr) || diff(p.ro, lro) {
				events = append(events, specEvent{eff: effOf(p.day), prio: 0, kind: "rate", rate: p.r, orate: p.ro, hasRate: true})
				lr, lro = p.r, p.ro
				first = false
			}
		}
	}
	// số dư
	{
		var last *big.Rat
		for _, p := range P {
			if last == nil || p.bal.Cmp(last) != 0 {
				events = append(events, specEvent{eff: effOf(p.day), prio: 1, kind: "bal", total: p.bal})
				last = p.bal
			}
		}
	}
	// quá hạn
	if contains(c.Product.OverdueCheck, "CM126.001") {
		od := optDay(c.Agreement.OverdueDate)
		ex := optDay(c.Agreement.ExtensionDate)
		var date *int
		extension := false
		if ex != nil && (od == nil || *ex > *od) {
			date, extension = ex, true
		} else if od != nil {
			date = od
		}
		if date != nil {
			grace := 0
			if c.Product.PrincipalGrace != nil && *c.Product.PrincipalGrace > 0 {
				grace = *c.Product.PrincipalGrace
			}
			start := *date
			if !extension {
				start += grace
			}
			if start <= T {
				effDate := start
				if effDate < F {
					effDate = F
				}
				// resolveBalanceAt trên danh sách đã xử lý P
				amt := zeroRat()
				if len(P) > 0 {
					var pick *mergedBal
					for i := range P {
						if P[i].day <= effDate && (pick == nil || P[i].day >= pick.day) {
							pick = &P[i]
						}
					}
					if pick == nil {
						pick = &P[0]
						for i := range P {
							if P[i].day < pick.day {
								pick = &P[i]
							}
						}
					}
					amt = pick.bal
				}
				events = append(events, specEvent{eff: effOf(effDate), prio: 2, kind: "ovd", amount: amt})
			}
		}
	}
	// trả gốc
	if pay := optRat(c.Calc.PrincipalPayment); pay != nil && pay.Sign() > 0 {
		pd := optDay(c.Calc.PaymentDate)
		if pd == nil {
			res.Err = "NONDETERMINISTIC_PAYMENT_DATE"
			return res
		}
		if *pd <= T {
			events = append(events, specEvent{eff: effOf(*pd), prio: 3, kind: "pay", amount: pay})
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].eff != events[j].eff {
			return events[i].eff < events[j].eff
		}
		return events[i].prio < events[j].prio
	})

	// quét từng ngày
	pn, po := zeroRat(), zeroRat()
	var r, ro *big.Rat
	ei := 0
	n := Tend - F + 1
	if n < 0 {
		n = 0
	}
	states := make([]dayState, 0, n)
	for d := F; d <= Tend; d++ {
		for ei < len(events) && events[ei].eff <= d {
			ev := events[ei]
			ei++
			switch ev.kind {
			case "rate":
				r, ro = ev.rate, ev.orate
			case "bal":
				if ev.total.Cmp(po) < 0 {
					po = new(big.Rat).Set(ev.total)
					pn = zeroRat()
				} else {
					pn = new(big.Rat).Sub(ev.total, po)
				}
			case "ovd":
				cur := new(big.Rat).Add(pn, po)
				a := new(big.Rat).Set(ev.amount)
				if a.Sign() < 0 {
					a = zeroRat()
				}
				if a.Cmp(cur) > 0 {
					a = cur
				}
				po = a
				pn = new(big.Rat).Sub(cur, a)
				if pn.Sign() < 0 {
					pn = zeroRat()
				}
			case "pay":
				op := ev.amount
				if po.Cmp(op) < 0 {
					op = po
				}
				po = new(big.Rat).Sub(po, op)
				rest := new(big.Rat).Sub(ev.amount, op)
				pn = new(big.Rat).Sub(pn, rest)
				if pn.Sign() < 0 {
					pn = zeroRat()
				}
			}
		}
		states = append(states, dayState{pn: pn, po: po, r: r, ro: ro})
	}

	// gom ngày dư nợ 0 vào ngày dương kế tiếp (hành vi Arda)
	if rl.lateFill || rl.gapFill {
		firstPos := -1
		for _, b := range src {
			if b.bal.Sign() > 0 {
				firstPos = b.day
				break
			}
		}
		i := 0
		for i < len(states) {
			tot := new(big.Rat).Add(states[i].pn, states[i].po)
			if tot.Sign() > 0 {
				i++
				continue
			}
			j := i
			for j < len(states) && new(big.Rat).Add(states[j].pn, states[j].po).Sign() <= 0 {
				j++
			}
			if j < len(states) {
				runEndDay := F + j - 1
				late := firstPos >= 0 && runEndDay < firstPos
				if (late && rl.lateFill) || (!late && rl.gapFill) {
					for k := i; k < j; k++ {
						states[k] = states[j]
					}
				}
			}
			i = j
		}
	}

	base := epasBaseDays(c.Product.BaseCode)
	if rl.base360 {
		base = 360
	}
	for _, s := range states {
		tot := new(big.Rat).Add(s.pn, s.po)
		if !rl.ovdSplit {
			if tot.Sign() > 0 && s.r != nil {
				res.Normal.Add(res.Normal, dayInterest(tot, s.r, base))
			}
			if tot.Sign() > 0 {
				res.Days++
			}
			continue
		}
		if pos(s.pn) && s.r != nil {
			res.Normal.Add(res.Normal, dayInterest(s.pn, s.r, base))
		}
		if pos(s.po) {
			if s.ro == nil {
				res.Err = "PARAMETER_CONFIG_NOT_FOUND"
				return res
			}
			res.Overdue.Add(res.Overdue, dayInterest(s.po, s.ro, base))
		}
		if tot.Sign() > 0 {
			res.Days++
		}
	}
	return res
}

func dayInterest(p, ratePercent *big.Rat, base int) *big.Rat {
	x := new(big.Rat).Mul(p, ratePercent)
	return x.Quo(x, big.NewRat(int64(100*base), 1))
}

// ------------------------------------------------------------------- làm tròn

func floorRat(x *big.Rat) *big.Int {
	q, m := new(big.Int).DivMod(x.Num(), x.Denom(), new(big.Int))
	_ = m
	return q // DivMod Euclidean với mẫu dương = floor
}

func ceilRat(x *big.Rat) *big.Int {
	f := floorRat(x)
	if new(big.Rat).SetInt(f).Cmp(x) == 0 {
		return f
	}
	return f.Add(f, big.NewInt(1))
}

func halfUpRat(x *big.Rat) *big.Int { // nửa tròn ra xa số 0 (HALF_UP của BigDecimal)
	half := big.NewRat(1, 2)
	if x.Sign() >= 0 {
		return floorRat(new(big.Rat).Add(x, half))
	}
	return new(big.Int).Neg(floorRat(new(big.Rat).Add(new(big.Rat).Neg(x), half)))
}

// roundIntended làm tròn theo ngữ nghĩa cấu hình (CM163: ROUND/CEIL/FLOOR hoặc mã CM163.001/.002/.003, CM164: ROUND_NO).
func roundIntended(x *big.Rat, roundNo *int, roundType *string) *big.Rat {
	if roundNo == nil || *roundNo <= 0 {
		return x
	}
	f := big.NewRat(int64(*roundNo), 1)
	q := new(big.Rat).Quo(x, f)
	mode := "ROUND"
	if roundType != nil {
		switch {
		case strings.Contains(*roundType, "CEIL") || strings.Contains(*roundType, "CM163.002"):
			mode = "CEIL"
		case strings.Contains(*roundType, "FLOOR") || strings.Contains(*roundType, "CM163.003"):
			mode = "FLOOR"
		}
	}
	var n *big.Int
	switch mode {
	case "CEIL":
		n = ceilRat(q)
	case "FLOOR":
		n = floorRat(q)
	default:
		n = halfUpRat(q)
	}
	return new(big.Rat).Mul(new(big.Rat).SetInt(n), f)
}

func roundVND(x *big.Rat) *big.Rat { return new(big.Rat).SetInt(halfUpRat(x)) }

// deductions: các khoản trừ EPAS áp lên lãi thô (lãi thừa thiếu, lãi đã thu khi tính từ ngày thu lãi ban đầu).
func ratOrZero(s *string) *big.Rat {
	if r := optRat(s); r != nil {
		return r
	}
	return zeroRat()
}

// finalTarget: số tiền cuối theo mục tiêu (làm tròn theo cấu hình sản phẩm, trừ lãi thừa thiếu / lãi đã thu như EPAS).
// withDeduct=false bỏ các khoản trừ (để đo riêng ảnh hưởng của chúng). Dự thu có tham số lãi chênh lệch = làm tròn riêng
// lãi trong hạn và quá hạn rồi cộng, trừ chênh lệch, dự thu đã ghi và lãi ngoại bảng.
func finalTarget(c *egCase, s specOut, intended bool) *big.Rat {
	return finalTargetOpt(c, s, intended, true)
}

func finalTargetOpt(c *egCase, s specOut, intended, withDeduct bool) *big.Rat {
	round := func(x *big.Rat) *big.Rat {
		if intended {
			return roundIntended(x, c.Product.RoundNo, c.Product.RoundType)
		}
		return roundVND(x)
	}
	param1 := c.Param.ApplyInterestDiff != nil && *c.Param.ApplyInterestDiff == "1"
	if c.CalcType == "INT_ACCR" && param1 {
		t := new(big.Rat).Add(round(s.Normal), round(s.Overdue))
		if withDeduct {
			t.Sub(t, ratOrZero(c.Agreement.InterestDiffAmt))
			if c.Agreement.DebtGroup != nil && *c.Agreement.DebtGroup == "GROUP1" {
				t.Sub(t, ratOrZero(c.Agreement.AcrInterestAmt))
			} else {
				t.Sub(t, ratOrZero(c.Agreement.OffBalDueInterestAmt))
			}
		}
		if t.Sign() < 0 {
			t = zeroRat()
		}
		return round(t)
	}
	raw := new(big.Rat).Add(s.Normal, s.Overdue)
	if withDeduct {
		if param1 && contains(c.Product.StartMethod, "CM125.001") {
			raw.Sub(raw, ratOrZero(c.Agreement.InterestDiffAmt))
		}
		if contains(c.Product.StartMethod, "CM125.002") {
			paid := ratOrZero(c.Agreement.ColnInterestAmt)
			if c.CalcType == "INT_ACCR" {
				paid.Add(paid, ratOrZero(c.Agreement.AcrInterestPeriodAmt))
				paid.Add(paid, ratOrZero(c.Agreement.AcrInterestPrevYearAmt))
			}
			raw.Sub(raw, paid)
			if raw.Sign() < 0 {
				raw = zeroRat()
			}
		}
	}
	return round(raw)
}

// ------------------------------------------------------------------- một ca

type egResult struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	AMinor     *int64            `json:"a_minor,omitempty"`
	ADecMinor  *int64            `json:"a_dec_minor,omitempty"`
	AExactRuns string            `json:"a_exact_runs,omitempty"`
	AExactAll  string            `json:"a_exact_all,omitempty"`
	ARuns      int               `json:"a_runs,omitempty"`
	ASkipped   int               `json:"a_skipped,omitempty"`
	ADays      int               `json:"a_days,omitempty"`
	ALast      string            `json:"a_last_accrual,omitempty"`
	AErr       string            `json:"a_err,omitempty"`
	TFinal     string            `json:"t_final,omitempty"`
	TVnd       string            `json:"t_vnd,omitempty"`
	TNormal    string            `json:"t_normal,omitempty"`
	TOverdue   string            `json:"t_overdue,omitempty"`
	TFirst     string            `json:"t_first_day,omitempty"`
	TDays      int               `json:"t_days,omitempty"`
	TErr       string            `json:"t_err,omitempty"`
	Validated  *bool             `json:"validated,omitempty"`
	Causes     []string          `json:"causes,omitempty"`
	Marginal   map[string]string `json:"marginal,omitempty"`
}

func runCase(c *egCase) egResult {
	res := egResult{ID: c.ID, Type: c.Type}
	switch c.Type {
	case "round":
		d, err := decimal.NewFromString(c.Amount)
		if err != nil {
			res.AErr = err.Error()
			return res
		}
		m, err := ardamoney.ToMinor(d, "VND")
		if err != nil {
			res.AErr = err.Error()
			return res
		}
		res.AMinor = &m
		return res
	case "fe_alloc":
		return res
	}
	D := dayNum(c.Agreement.DisburseDate)
	T := dayNum(c.Calc.CalculateDate)
	L, why := ardaLastAccrual(c)
	exactAll := zeroRat()
	if why != "" {
		res.AErr = why
	} else {
		if L != nil {
			res.ALast = dayStr(*L)
		}
		sim := ardaSim(c, L, D, T)
		m, dm := sim.Minor, sim.DecMinor
		res.AMinor, res.ADecMinor = &m, &dm
		res.AExactRuns = ratStr(sim.ExactRuns)
		res.AExactAll = ratStr(sim.ExactAll)
		exactAll = sim.ExactAll
		res.ARuns, res.ASkipped, res.ADays = sim.Runs, sim.Skipped, sim.Days
	}

	// bộ tính mục tiêu và phân rã nguyên nhân
	epas := epasRules(c)
	t0 := specRun(c, epas)
	if t0.Err != "" {
		res.TErr = t0.Err
		return res
	}
	tf := finalTarget(c, t0, true)
	res.TFinal = ratStr(tf)
	res.TVnd = ratStr(finalTarget(c, t0, false))
	res.TNormal, res.TOverdue = ratStr(t0.Normal), ratStr(t0.Overdue)
	res.TFirst = dayStr(t0.First)
	res.TDays = t0.Days
	if res.AMinor == nil {
		return res
	}

	arda := ardaRules()
	aSpec := specRun(c, arda)
	if aSpec.Err == "" {
		ok := new(big.Rat).Add(aSpec.Normal, aSpec.Overdue).Cmp(exactAll) == 0
		res.Validated = &ok
	}

	val := func(rl rules, intended bool) *big.Rat {
		s := specRun(c, rl)
		if s.Err != "" {
			return nil
		}
		return finalTarget(c, s, intended)
	}
	toggles := []string{"firstDay", "settle", "lateFill", "gapFill", "shift", "base", "overdue"}
	setK := func(rl rules, k string, v rules) rules {
		switch k {
		case "firstDay":
			rl.firstDay = v.firstDay
		case "settle":
			rl.settle = v.settle
		case "lateFill":
			rl.lateFill = v.lateFill
		case "gapFill":
			rl.gapFill = v.gapFill
		case "shift":
			rl.shift = v.shift
		case "base":
			rl.base360 = v.base360
		case "overdue":
			rl.ovdSplit = v.ovdSplit
		}
		return rl
	}
	res.Marginal = map[string]string{}
	allArda := val(arda, false)
	for _, k := range toggles {
		single := val(setK(epas, k, arda), true) // chỉ lật k về kiểu Arda
		leave := val(setK(arda, k, epas), false) // giữ k kiểu EPAS, còn lại kiểu Arda
		active := false
		if single != nil && tf != nil && single.Cmp(tf) != 0 {
			active = true
			res.Marginal[k+"_single"] = ratStr(new(big.Rat).Sub(single, tf))
		}
		if leave != nil && allArda != nil && leave.Cmp(allArda) != 0 {
			active = true
			res.Marginal[k+"_leave"] = ratStr(new(big.Rat).Sub(leave, allArda))
		}
		if active {
			res.Causes = append(res.Causes, k)
		}
	}
	// khoản trừ lãi thừa thiếu / lãi đã thu / dự thu đã ghi (Arda không có)
	if tfNo := finalTargetOpt(c, t0, true, false); tfNo.Cmp(tf) != 0 {
		res.Causes = append(res.Causes, "deduct")
	}
	// làm tròn theo ROUND_NO của sản phẩm (Arda làm tròn về 1 đồng)
	xE := new(big.Rat).Add(t0.Normal, t0.Overdue)
	xA := new(big.Rat).Add(aSpec.Normal, aSpec.Overdue)
	if roundIntended(xE, c.Product.RoundNo, c.Product.RoundType).Cmp(roundVND(xE)) != 0 ||
		roundIntended(xA, c.Product.RoundNo, c.Product.RoundType).Cmp(roundVND(xA)) != 0 {
		res.Causes = append(res.Causes, "roundNo")
		// chênh lệch (kiểu Arda trừ kiểu EPAS) nếu chỉ thay làm tròn theo ROUND_NO bằng làm tròn 1 đồng
		res.Marginal["roundNo_single"] = ratStr(new(big.Rat).Sub(roundVND(xE), roundIntended(xE, c.Product.RoundNo, c.Product.RoundType)))
	}
	// làm tròn từng ngày / bỏ lần chạy có lãi 0 (Arda) so với làm tròn một lần
	if allArda != nil && new(big.Rat).SetInt64(*res.ADecMinor).Cmp(allArda) != 0 {
		res.Causes = append(res.Causes, "perRun")
		res.Marginal["perRun_single"] = ratStr(new(big.Rat).Sub(new(big.Rat).SetInt64(*res.ADecMinor), allArda))
	}
	// float64
	if *res.AMinor != *res.ADecMinor {
		res.Causes = append(res.Causes, "float64")
		res.Marginal["float64_single"] = fmt.Sprint(*res.AMinor - *res.ADecMinor)
	}
	return res
}

func sim0(s string) *big.Rat {
	if s == "" {
		return zeroRat()
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return zeroRat()
	}
	return r
}

// ------------------------------------------------------------------- chạy / fixture

func readCases(t *testing.T, path string) []*egCase {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("không mở được %s: %v", path, err)
	}
	defer f.Close()
	var out []*egCase
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		c := &egCase{}
		if err := json.Unmarshal([]byte(line), c); err != nil {
			t.Fatalf("vector hỏng: %v: %.120s", err, line)
		}
		out = append(out, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEpasDiffRun(t *testing.T) {
	in, out := os.Getenv("EPAS_DIFF_IN"), os.Getenv("EPAS_DIFF_OUT")
	if in == "" || out == "" {
		t.Skip("đặt EPAS_DIFF_IN và EPAS_DIFF_OUT để chạy differential test P3-LNM")
	}
	cases := readCases(t, in)
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	for _, c := range cases {
		b, err := json.Marshal(runCase(c))
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
		w.WriteByte('\n')
	}
}

// fixtureRow là một dòng testdata/epas-oracle-lnm.jsonl: vector + kết quả chuẩn EPAS + dấu vân tay Arda đã ghi.
type fixtureRow struct {
	Source  string          `json:"source"`
	Case    json.RawMessage `json:"case"`
	Epas    json.RawMessage `json:"epas"`
	Class   string          `json:"class"`
	Cluster string          `json:"cluster"`
	ArdaWas *int64          `json:"arda_minor"`
	TFinal  string          `json:"t_final"`
	EpasTot string          `json:"epas_total"`
	EpasOK  bool            `json:"epas_ok"`
}

// TestEpasGoldenFixture khóa hành vi hiện tại của Arda so với oracle EPAS:
//   - ca "khop": Arda phải bằng EPAS;
//   - mọi ca so được: Arda hiện tại phải đúng giá trị đã ghi (đổi hành vi thì test báo để cập nhật có chủ đích);
//   - bộ tính mục tiêu (G2) phải bằng EPAS ở các ca không có "lech_epas".
//
// EPAS_GOLDEN_STRICT=1: yêu cầu Arda bằng EPAS ở mọi ca so được (bật khi T9.1 hoàn tất).
func TestEpasGoldenFixture(t *testing.T) {
	path := os.Getenv("EPAS_GOLDEN_FIXTURE")
	if path == "" {
		path = "testdata/epas-oracle-lnm.jsonl"
	}
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("chưa có fixture: %v", err)
	}
	defer f.Close()
	strict := os.Getenv("EPAS_GOLDEN_STRICT") == "1"
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row fixtureRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("dòng fixture hỏng: %v", err)
		}
		if !strings.HasPrefix(row.Source, "epas-oracle@") {
			t.Fatalf("thiếu trường nguồn epas-oracle@<sha9>: %q", row.Source)
		}
		c := &egCase{}
		if err := json.Unmarshal(row.Case, c); err != nil {
			t.Fatal(err)
		}
		n++
		t.Run(c.ID, func(t *testing.T) {
			if !row.EpasOK {
				t.Skip("EPAS từ chối tính ca này (ngoại lệ nghiệp vụ), không có số chuẩn")
			}
			if c.Type == "fe_alloc" {
				t.Skip("chia lãi theo tỷ lệ vốn: Arda chưa có hàm tương đương (chỉ dùng để so BE với FE)")
			}
			got := runCase(c)
			if got.AMinor == nil {
				t.Fatalf("Arda không tính được (%s)", got.AErr)
			}
			if row.ArdaWas != nil && *got.AMinor != *row.ArdaWas {
				t.Errorf("Arda đổi hành vi: nay %d, fixture ghi %d (cụm %s)", *got.AMinor, *row.ArdaWas, row.Cluster)
			}
			epasTotal := sim0(row.EpasTot)
			if row.Class == "khop" || strict {
				if new(big.Rat).SetInt64(*got.AMinor).Cmp(epasTotal) != 0 {
					t.Errorf("Arda %d khác EPAS %s (lớp %s, cụm %s)", *got.AMinor, row.EpasTot, row.Class, row.Cluster)
				}
			}
			if row.Class != "loi_epas" && got.TFinal != "" {
				d := new(big.Rat).Sub(sim0(got.TFinal), epasTotal)
				tol := big.NewRat(1, 1000000)
				if d.Abs(d).Cmp(tol) > 0 {
					t.Errorf("bộ tính mục tiêu %s lệch EPAS %s", got.TFinal, row.EpasTot)
				}
			}
		})
	}
	if n == 0 {
		t.Fatal("fixture rỗng")
	}
}
