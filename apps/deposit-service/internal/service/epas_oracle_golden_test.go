//go:build epasgolden

package service

// Differential test tinh lai tien gui: EPAS DpmInterestCalculator (oracle) vs Arda interest.Calculate.
// Build tag `epasgolden` de khong lam gay CI thuong. Hai che do:
//
//  1. Dump:    P3_DPM_VECTORS=<vectors.jsonl> P3_DPM_OUT=<arda-results.jsonl> \
//              go test -tags epasgolden -run TestEpasOracleDump ./internal/service/
//  2. Fixture: go test -tags epasgolden -run TestEpasOracleFixture ./internal/service/
//     doc testdata/epas_oracle_fixture.jsonl (truong "src" = epas-oracle@<sha9 be_dpm>).
//
// Sau T9.1 (G2-DPM-NGAY-01 nua mo, RatePoint, lam tron mot lan chinh xac) Arda co mot duong tinh duy nhat:
//   r  = interest.Calculate voi khoang nua mo [from,to), diem so du va moc lai suat (RatePoints) cua hop dong;
//   s, sx = bi danh cua r, giu lai de cong cu so sanh P3 (compare.mjs) van doc duoc tep dump;
//   daily = mo phong RunDaily phuong an B (du chi luy ke theo ROUND_NO/CM163 cua san pham, quyet dinh 2026-10-18):
//           moi ngay nghiep vu mot lan computeCumulativeAccrual; tong sau n ngay phai bang lai mot lan cua EPAS.
// Che do EPAS_GOLDEN_STRICT=1: moi ca fixture phai khop EPAS o che do r.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/interest"
	"github.com/shopspring/decimal"
)

type p3Point struct {
	D string `json:"d"`
	B string `json:"b,omitempty"`
	R string `json:"r,omitempty"`
}

type p3Vector struct {
	ID         string    `json:"id"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Rate       string    `json:"rate"`
	Denom      *int      `json:"denom"`
	RoundNo    *int      `json:"roundNo"`
	RoundType  *string   `json:"roundType"`
	Balances   []p3Point `json:"balances"`
	RatePoints []p3Point `json:"ratePoints"`
}

type p3Seg struct {
	D   int64  `json:"d"`
	Bal string `json:"bal"`
	Amt string `json:"amt"`
}

type p3Calc struct {
	Days    int64   `json:"days"`
	Raw     string  `json:"raw"`
	Rounded string  `json:"rounded"`
	Segs    []p3Seg `json:"segs"`
}

type p3Daily struct {
	Days  int64 `json:"days"`
	Minor int64 `json:"minor"` // tong lai da lam tron den dong moi ngay (Rounded.Round(0))
	Skip  int   `json:"skip"`  // so ngay bi bo (rate 0 hoac amount <= 0, giong RunDaily)
}

type p3Out struct {
	ID    string   `json:"id"`
	R     *p3Calc  `json:"r"`
	S     *p3Calc  `json:"s"`
	SX    *p3Calc  `json:"sx"`
	Daily *p3Daily `json:"daily"`
}

func p3Date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func p3Dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(fmt.Sprintf("so khong hop le %q: %v", s, err))
	}
	return d
}

func p3Dur(from, to time.Time) int64 { return int64(to.Sub(from) / (24 * time.Hour)) }

func p3Convert(r interest.Result) *p3Calc {
	out := &p3Calc{Days: r.TotalDays, Raw: r.Raw.String(), Rounded: r.Rounded.String(), Segs: []p3Seg{}}
	for _, s := range r.Segments {
		out.Segs = append(out.Segs, p3Seg{D: s.Days, Bal: s.Balance.String(), Amt: s.Amount.String()})
	}
	return out
}

// p3RateAt: lai suat hieu luc tai ngay (moc gan nhat co date <= ngay, khong co thi lai suat mac dinh).
// Cung ngay: moc xuat hien sau thang (sap xep on dinh theo ngay, giong EPAS).
func p3RateAt(def decimal.Decimal, rps []p3Point, at time.Time) decimal.Decimal {
	sorted := append([]p3Point(nil), rps...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].D < sorted[j].D })
	v := def
	for _, rp := range sorted {
		if !p3Date(rp.D).After(at) {
			v = p3Dec(rp.R)
		}
	}
	return v
}

func p3Points(bs []p3Point) []interest.BalancePoint {
	out := make([]interest.BalancePoint, 0, len(bs))
	for _, b := range bs {
		out = append(out, interest.BalancePoint{Date: p3Date(b.D), Balance: p3Dec(b.B)})
	}
	return out
}

func p3Balance(bs []p3Point, at time.Time) decimal.Decimal {
	sorted := append([]p3Point(nil), bs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].D < sorted[j].D })
	v := decimal.Zero
	for _, b := range sorted {
		if !p3Date(b.D).After(at) {
			v = p3Dec(b.B)
		}
	}
	return v
}

func p3RatePoints(rps []p3Point) []interest.RatePoint {
	out := make([]interest.RatePoint, 0, len(rps))
	for _, rp := range rps {
		out = append(out, interest.RatePoint{Date: p3Date(rp.D), Rate: p3Dec(rp.R)})
	}
	return out
}

func p3Input(v p3Vector, from, to time.Time, rate decimal.Decimal) interest.Input {
	in := interest.Input{From: from, To: to, Rate: rate, Points: p3Points(v.Balances), RatePoints: p3RatePoints(v.RatePoints)}
	if v.Denom != nil {
		in.BaseDenominator = *v.Denom
	}
	if v.RoundNo != nil {
		in.RoundNo = *v.RoundNo
	}
	if v.RoundType != nil {
		in.RoundType = *v.RoundType
	}
	return in
}

func p3RunArda(v p3Vector) p3Out {
	from, to := p3Date(v.From), p3Date(v.To)
	def := p3Dec(v.Rate)
	out := p3Out{ID: v.ID}

	// r: duong tinh san xuat (nua mo, RatePoints, lam tron mot lan); s va sx la bi danh.
	r := p3Convert(interest.Calculate(p3Input(v, from, to, def)))
	out.R, out.S, out.SX = r, r, r

	// daily: mo phong RunDaily theo phuong an B (du chi luy ke) cho moi ngay nghiep vu [from, to-1]: moi ngay mot lan
	// computeCumulativeAccrual (loi ra neu san pham thieu ROUND_NO/ROUND_TYPE), so am hoac 0 bi bo, so duong cong vao
	// "da du chi" cua ngay sau. Khong mo phong duoc khi vector khong co cau hinh lam tron hop le.
	if from.Before(to) && v.RoundNo != nil && v.RoundType != nil && *v.RoundNo >= 0 {
		d := &p3Daily{}
		denom := 0
		if v.Denom != nil {
			denom = *v.Denom
		}
		// Diem so du sau ngay from thanh cac khoan gui them (co the am); so du ban dau la so du tai from.
		sortedBal := append([]p3Point(nil), v.Balances...)
		sort.SliceStable(sortedBal, func(i, j int) bool { return sortedBal[i].D < sortedBal[j].D })
		opening := p3Balance(v.Balances, from)
		running := opening
		var topUps []topUp
		for _, b := range sortedBal {
			bd := p3Date(b.D)
			if !bd.After(from) {
				continue
			}
			next := p3Dec(b.B)
			topUps = append(topUps, topUp{Date: bd, Amount: next.Sub(running)})
			running = next
		}
		var ratePoints []interest.RatePoint
		for _, rp := range v.RatePoints {
			if p3Date(rp.D).After(from) {
				ratePoints = append(ratePoints, interest.RatePoint{Date: p3Date(rp.D), Rate: p3Dec(rp.R)})
			}
		}
		in := cumulativeAccrualInput{
			PeriodStart:      from,
			CurrentPrincipal: running,
			TopUps:           topUps,
			Rate:             p3RateAt(def, v.RatePoints, from),
			RatePoints:       ratePoints,
			Denominator:      denom,
			RoundNo:          v.RoundNo,
			RoundType:        v.RoundType,
		}
		var total int64
		for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
			in.BusinessDate = day
			in.PriorAccrued = total
			res, err := computeCumulativeAccrual(in)
			if err != nil || res.Amount <= 0 {
				d.Skip++
				continue
			}
			total += res.Amount
		}
		d.Days = p3Dur(from, to)
		d.Minor = total
		out.Daily = d
	}
	return out
}

// p3NonNegative: vector khong co so du hoac lai suat am (khong hop le nghiep vu, EPAS cong ca so am).
func p3NonNegative(v p3Vector) bool {
	for _, b := range v.Balances {
		if p3Dec(b.B).Sign() < 0 {
			return false
		}
	}
	if p3Dec(v.Rate).Sign() < 0 {
		return false
	}
	for _, r := range v.RatePoints {
		if p3Dec(r.R).Sign() < 0 {
			return false
		}
	}
	return true
}

func p3ReadLines(t *testing.T, path string, fn func(line []byte)) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("khong mo duoc %s: %v", path, err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			fn(append([]byte(nil), sc.Bytes()...))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestEpasOracleDump(t *testing.T) {
	in, outPath := os.Getenv("P3_DPM_VECTORS"), os.Getenv("P3_DPM_OUT")
	if in == "" || outPath == "" {
		t.Skip("dat P3_DPM_VECTORS va P3_DPM_OUT de sinh arda-results.jsonl")
	}
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	n := 0
	p3ReadLines(t, in, func(line []byte) {
		var v p3Vector
		if err := json.Unmarshal(line, &v); err != nil {
			t.Fatalf("vector hong: %v", err)
		}
		b, err := json.Marshal(p3RunArda(v))
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
		w.WriteByte('\n')
		n++
	})
	t.Logf("arda-results: %d ca -> %s", n, outPath)
}

// ---- fixture ----

type p3FixtureCalc struct {
	Days    int64  `json:"days"`
	Raw     string `json:"raw"`
	Rounded string `json:"rounded"`
}

type p3FixtureArda struct {
	Days    int64  `json:"days"`
	Raw     string `json:"raw"`
	Rounded string `json:"rounded"`
}

type p3Fixture struct {
	p3Vector
	Src      string        `json:"src"`
	Class    string        `json:"class"` // match | g2 | arda_bug | not_comparable
	Cluster  string        `json:"cluster"`
	Epas     p3FixtureCalc `json:"epas"`
	ArdaSeen p3FixtureArda `json:"ardaSeen"` // gia tri Arda (che do r) luc sinh fixture
	// ArdaSeenSX: gia tri Arda che do sx luc sinh fixture (chi dung cho class arda_bug).
	ArdaSeenSX p3FixtureArda `json:"ardaSeenSx"`
}

func p3Same(a, b string) bool { return p3Dec(a).Equal(p3Dec(b)) }

// TestEpasOracleFixture: Arda chay lai tren fixture oracle EPAS.
//   - class "match": che do r phai KHOP EPAS.
//   - class "g2": chenh lech da duoc chot (Arda cu: [from,to] / mot lai suat); che do sx (nua mo + chia moc lai suat)
//     phai KHOP EPAS; che do r duoc phep bang EPAS (da sua theo G2) hoac bang gia tri cu ghi trong fixture.
//   - class "arda_bug": che do sx cua Arda phai bang EPAS (da sua) hoac bang gia tri sai da ghi (khoa hanh vi).
func TestEpasOracleFixture(t *testing.T) {
	path := "testdata/epas_oracle_fixture.jsonl"
	if _, err := os.Stat(path); err != nil {
		t.Skip("chua co fixture")
	}
	strict := os.Getenv("EPAS_GOLDEN_STRICT") == "1"
	n, matched, dailyCmp, dailyMatched := 0, 0, 0, 0
	p3ReadLines(t, path, func(line []byte) {
		var fx p3Fixture
		if err := json.Unmarshal(line, &fx); err != nil {
			t.Fatalf("fixture hong: %v", err)
		}
		n++
		got := p3RunArda(fx.p3Vector)
		eq := func(c *p3Calc) bool {
			return c.Days == fx.Epas.Days && p3Same(c.Raw, fx.Epas.Raw) && p3Same(c.Rounded, fx.Epas.Rounded)
		}
		seen := func(c *p3Calc) bool {
			return c.Days == fx.ArdaSeen.Days && p3Same(c.Raw, fx.ArdaSeen.Raw) && p3Same(c.Rounded, fx.ArdaSeen.Rounded)
		}
		if got.Daily != nil && fx.Class != "arda_bug" && p3NonNegative(fx.p3Vector) {
			dailyCmp++
			if got.Daily.Minor == p3Dec(fx.Epas.Rounded).Round(0).IntPart() && p3Dec(fx.Epas.Rounded).IsInteger() {
				dailyMatched++
			} else if fx.RoundNo != nil && *fx.RoundNo > 0 {
				t.Errorf("%s: du chi luy ke %d != EPAS %s sau %d ngay", fx.ID, got.Daily.Minor, fx.Epas.Rounded, got.Daily.Days)
			}
		}
		if eq(got.R) {
			matched++
		} else if strict {
			t.Errorf("%s: strict: Arda %+v != EPAS %+v (lop %s, cum %s)", fx.ID, *got.R, fx.Epas, fx.Class, fx.Cluster)
		}
		switch fx.Class {
		case "match":
			if !eq(got.R) {
				t.Errorf("%s: Arda %+v != EPAS %+v", fx.ID, *got.R, fx.Epas)
			}
		case "g2":
			if !eq(got.SX) {
				t.Errorf("%s: Arda nua mo+chia moc %+v != EPAS %+v (so hoc lech ngoai G2)", fx.ID, *got.SX, fx.Epas)
			}
			if !eq(got.R) && !seen(got.R) {
				t.Errorf("%s: Arda doi hanh vi: %+v (EPAS %+v, cu %+v)", fx.ID, *got.R, fx.Epas, fx.ArdaSeen)
			}
		case "arda_bug":
			seenSX := got.SX.Days == fx.ArdaSeenSX.Days && p3Same(got.SX.Raw, fx.ArdaSeenSX.Raw) && p3Same(got.SX.Rounded, fx.ArdaSeenSX.Rounded)
			if !eq(got.SX) && !seenSX {
				t.Errorf("%s: Arda doi hanh vi (sx): %+v (EPAS %+v, loi da biet %+v)", fx.ID, *got.SX, fx.Epas, fx.ArdaSeenSX)
			}
		}
	})
	t.Logf("fixture: %d ca, che do r khop EPAS: %d/%d; du chi luy ke (RunDaily B) khop EPAS: %d/%d", n, matched, n, dailyMatched, dailyCmp)
}
