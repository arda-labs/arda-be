package service

// RunDaily theo phương án B (dự chi lũy kế) chạy trên kho giả trong bộ nhớ: mọi trạng thái của lần chạy nằm trong các
// dòng đã lưu (dòng dự chi, op trả/nhập gốc, giao dịch gửi thêm, lịch sử lãi suất), không giữ gì trong bộ nhớ giữa
// các lần chạy. Ví dụ chuẩn: 100 triệu, 6%/năm, cơ sở 365, ROUND_NO 1.000 HALF_UP, 30 ngày → tổng 493.000,
// các ngày đầu 16.000, 17.000, 16.000, 17.000.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/deposit-service/internal/interest"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

type fakeStore struct {
	savings  []repository.Savings
	products map[string]*repository.SavingsProduct
	rates    []repository.InterestRate
	accruals []*repository.Accrual
	periods  map[string]string // savingsID → mốc đầu kỳ (effective_date của op trả/nhập gốc gần nhất)
	topUps   map[string][]repository.TopUp
	seq      int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		products: map[string]*repository.SavingsProduct{},
		periods:  map[string]string{},
		topUps:   map[string][]repository.TopUp{},
	}
}

func (f *fakeStore) ListActiveSavingsForAccrual(context.Context, string) ([]repository.Savings, error) {
	out := make([]repository.Savings, len(f.savings))
	copy(out, f.savings)
	return out, nil
}

func (f *fakeStore) FirstPendingAccrual(_ context.Context, _, savingsID string) (*repository.Accrual, error) {
	for _, a := range f.accruals {
		if a.SavingsID == savingsID && a.Status != "POSTED" {
			return a, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) LastAccrualPeriod(_ context.Context, _, savingsID string) (string, error) {
	last := ""
	for _, a := range f.accruals {
		if a.SavingsID == savingsID && (a.Status == "POSTED" || a.JournalID != nil) && a.PeriodTo > last {
			last = a.PeriodTo
		}
	}
	return last, nil
}

func (f *fakeStore) InterestPeriodStart(_ context.Context, _, savingsID string) (string, error) {
	return f.periods[savingsID], nil
}

func (f *fakeStore) SumPeriodAccruals(_ context.Context, _, savingsID, periodStart string) (int64, error) {
	var total int64
	for _, a := range f.accruals {
		if a.SavingsID == savingsID && a.PeriodFrom >= periodStart {
			total += a.AmountMinor
		}
	}
	return total, nil
}

func (f *fakeStore) ListPostedTopUps(_ context.Context, _, savingsID, from string) ([]repository.TopUp, error) {
	var out []repository.TopUp
	for _, t := range f.topUps[savingsID] {
		if t.Date >= from {
			out = append(out, t)
		}
	}
	return out, nil
}

// candidates mirrors the SQL of FindEffectiveRate / ListRateChangeDates.
func (f *fakeStore) candidates(productCode string) []repository.InterestRate {
	var out []repository.InterestRate
	for _, r := range f.rates {
		if r.IsActive && (r.ProductCode == productCode || r.ProductCode == "") {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeStore) FindEffectiveRate(_ context.Context, _, productCode string, _ int, onDate string) (*repository.InterestRate, error) {
	var best *repository.InterestRate
	for _, r := range f.candidates(productCode) {
		if r.EffectiveFrom > onDate {
			continue
		}
		r := r
		if best == nil ||
			(r.ProductCode == productCode) != (best.ProductCode == productCode) && r.ProductCode == productCode ||
			(r.ProductCode == productCode) == (best.ProductCode == productCode) && r.EffectiveFrom > best.EffectiveFrom {
			best = &r
		}
	}
	return best, nil
}

func (f *fakeStore) ListRateChangeDates(_ context.Context, _, productCode string, _ int, after, through string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, r := range f.candidates(productCode) {
		if r.EffectiveFrom > after && r.EffectiveFrom <= through && !seen[r.EffectiveFrom] {
			seen[r.EffectiveFrom] = true
			out = append(out, r.EffectiveFrom)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeStore) GetProductByCode(_ context.Context, _, code string) (*repository.SavingsProduct, error) {
	return f.products[code], nil
}

func (f *fakeStore) CreateAccrual(_ context.Context, a *repository.Accrual) (bool, error) {
	for _, x := range f.accruals {
		if x.SavingsID == a.SavingsID && x.PeriodTo == a.PeriodTo {
			return false, nil // UNIQUE (tenant_id, savings_id, period_to)
		}
	}
	f.seq++
	a.ID = fmt.Sprintf("acc-%03d", f.seq)
	a.Status = "PENDING"
	cp := *a
	f.accruals = append(f.accruals, &cp)
	return true, nil
}

func (f *fakeStore) MarkAccrualPosted(_ context.Context, _, id, journalEntryID string) error {
	for _, a := range f.accruals {
		if a.ID == id && a.Status != "POSTED" {
			a.Status = "POSTED"
			a.JournalID = &journalEntryID
			for i := range f.savings {
				if f.savings[i].ID == a.SavingsID {
					f.savings[i].AccruedMinor += a.AmountMinor
				}
			}
		}
	}
	return nil
}

type dailyHarness struct {
	t     *testing.T
	store *fakeStore
	svc   *InterestService
	posts map[string]int // khóa GL → số lần gọi
}

func newDailyHarness(t *testing.T) *dailyHarness {
	h := &dailyHarness{t: t, store: newFakeStore(), posts: map[string]int{}}
	h.svc = &InterestService{accruals: h.store}
	h.svc.postGL = func(_ context.Context, _, _, idemKey, _ string, _ *repository.Savings, _ int64, _, _, _ string) (string, error) {
		h.posts[idemKey]++
		return "gl-" + idemKey, nil
	}
	return h
}

func (h *dailyHarness) addProduct(code string, roundNo *int, roundType *string) {
	h.store.products[code] = &repository.SavingsProduct{Code: code, InterestRate: 6, RoundNo: roundNo, RoundType: roundType}
}

func (h *dailyHarness) addSavings(id, product string, open string, principal int64) {
	h.store.savings = append(h.store.savings, repository.Savings{
		ID: id, SavingsCode: id, ProductCode: product, OpenDate: open, PrincipalMinor: principal, Status: "ACTIVE",
	})
}

func (h *dailyHarness) addRate(product, from string, rate float64) {
	h.store.rates = append(h.store.rates, repository.InterestRate{
		ProductCode: product, Denominator: 365, Rate: rate, EffectiveFrom: from, IsActive: true,
	})
}

func (h *dailyHarness) run(date string) (int, error) {
	h.t.Helper()
	return h.svc.RunDaily(context.Background(), "t1", date)
}

func (h *dailyHarness) mustRun(date string) int {
	h.t.Helper()
	posted, err := h.run(date)
	if err != nil {
		h.t.Fatalf("RunDaily(%s): %v", date, err)
	}
	return posted
}

func (h *dailyHarness) amounts(savingsID string) []int64 {
	var out []int64
	for _, a := range h.store.accruals {
		if a.SavingsID == savingsID {
			out = append(out, a.AmountMinor)
		}
	}
	return out
}

func (h *dailyHarness) accrued(savingsID string) int64 {
	for _, s := range h.store.savings {
		if s.ID == savingsID {
			return s.AccruedMinor
		}
	}
	return 0
}

func isoDay(start time.Time, offset int) string { return start.AddDate(0, 0, offset).Format("2006-01-02") }

func (h *dailyHarness) runDays(start string, n int) {
	h.t.Helper()
	first, _ := time.Parse("2006-01-02", start)
	for i := 0; i < n; i++ {
		h.mustRun(isoDay(first, i))
	}
}

func TestRunDailyCumulativeBriefExamples(t *testing.T) {
	for _, tc := range []struct {
		principal int64
		total     int64
		head      []int64
	}{
		{100_000_000, 493_000, []int64{16_000, 17_000, 16_000, 17_000}},
		{10_000_000, 49_000, []int64{2_000, 1_000, 2_000, 2_000}},
	} {
		t.Run(fmt.Sprint(tc.principal), func(t *testing.T) {
			h := newDailyHarness(t)
			h.addProduct("P1", intp(1000), strp(interest.RoundHalfUp))
			h.addRate("P1", "2026-01-01", 6)
			h.addSavings("S1", "P1", "2026-01-01", tc.principal)
			h.runDays("2026-01-01", 30)
			got := h.amounts("S1")
			for i, want := range tc.head {
				if got[i] != want {
					t.Fatalf("các ngày đầu = %v, muốn %v", got[:len(tc.head)], tc.head)
				}
			}
			if h.accrued("S1") != tc.total {
				t.Fatalf("tổng dự chi sau 30 ngày = %d, muốn %d", h.accrued("S1"), tc.total)
			}
			for key, n := range h.posts {
				if n != 1 || !strings.HasPrefix(key, "dpm-accrual-acc-") {
					t.Fatalf("khóa GL %s gọi %d lần; khóa phải theo id dòng và gọi đúng một lần", key, n)
				}
			}
		})
	}
}

// Chạy lại cùng ngày idempotent: không dòng mới, không bút toán mới, không dự chi hai lần.
func TestRunDailyIsIdempotentForTheSameDay(t *testing.T) {
	h := newDailyHarness(t)
	h.addProduct("P1", intp(1000), strp(interest.RoundHalfUp))
	h.addRate("P1", "2026-01-01", 6)
	h.addSavings("S1", "P1", "2026-01-01", 100_000_000)
	h.runDays("2026-01-01", 3)
	rows, accrued, posts := len(h.store.accruals), h.accrued("S1"), len(h.posts)
	if posted := h.mustRun("2026-01-03"); posted != 0 {
		t.Fatalf("chạy lại cùng ngày posted = %d, muốn 0", posted)
	}
	if len(h.store.accruals) != rows || h.accrued("S1") != accrued || len(h.posts) != posts {
		t.Fatalf("chạy lại làm đổi trạng thái: %d dòng, dự chi %d, %d bút toán", len(h.store.accruals), h.accrued("S1"), len(h.posts))
	}
}

// Sổ nhỏ có ngày dự chi 0 (không dòng): cửa sổ vẫn liền kỳ, tổng cả kỳ vẫn bằng lãi một lần.
func TestRunDailySmallBalanceSkipsZeroDaysWithoutLosingThem(t *testing.T) {
	h := newDailyHarness(t)
	h.addProduct("P1", intp(1000), strp(interest.RoundHalfUp))
	h.addRate("P1", "2026-01-01", 6)
	h.addSavings("S1", "P1", "2026-01-01", 1_000_000)
	h.runDays("2026-01-01", 30)
	if len(h.store.accruals) >= 30 {
		t.Fatalf("1 triệu với bước 1.000 phải có ngày không sinh dòng, nhưng có %d dòng", len(h.store.accruals))
	}
	if h.accrued("S1") != 5_000 {
		t.Fatalf("tổng = %d, muốn 5000", h.accrued("S1"))
	}
	prevTo := "2025-12-31"
	totalDays := 0
	for _, a := range h.store.accruals {
		next, _ := time.Parse("2006-01-02", prevTo)
		if a.PeriodFrom != next.AddDate(0, 0, 1).Format("2006-01-02") {
			t.Fatalf("dòng bắt đầu %s sau dòng kết thúc %s: hở hoặc chồng ngày", a.PeriodFrom, prevTo)
		}
		prevTo = a.PeriodTo
		totalDays += a.Days
	}
	if totalDays == 0 {
		t.Fatal("không có ngày nào")
	}
}

// Sổ đã dự chi bằng làm tròn đồng mỗi ngày: giữ nguyên số đã ghi, ngày đầu chạy theo B tự bù.
func TestRunDailyAbsorbsLegacyPerDayRows(t *testing.T) {
	h := newDailyHarness(t)
	h.addProduct("P1", intp(1000), strp(interest.RoundHalfUp))
	h.addRate("P1", "2026-01-01", 6)
	h.addSavings("S1", "P1", "2026-01-01", 100_000_000)
	first, _ := time.Parse("2006-01-02", "2026-01-01")
	for i := 0; i < 10; i++ {
		day := isoDay(first, i)
		j := "gl-legacy"
		h.store.accruals = append(h.store.accruals, &repository.Accrual{
			ID: fmt.Sprintf("old-%d", i), SavingsID: "S1", PeriodFrom: day, PeriodTo: day, Days: 1,
			AmountMinor: 16_438, Status: "POSTED", JournalID: &j,
		})
	}
	h.mustRun("2026-01-11")
	got := h.amounts("S1")
	if got[len(got)-1] != 16_620 {
		t.Fatalf("ngày đầu bù = %d, muốn 16620", got[len(got)-1])
	}
}

// Nhập gốc/trả lãi mở kỳ mới: chỉ dòng của kỳ mới tính vào "đã dự chi", không cộng chéo kỳ.
func TestRunDailyNewPeriodAfterCapitalizeDoesNotCrossPeriods(t *testing.T) {
	h := newDailyHarness(t)
	h.addProduct("P1", intp(1000), strp(interest.RoundHalfUp))
	h.addRate("P1", "2026-01-01", 6)
	h.addSavings("S1", "P1", "2026-01-01", 100_000_000)
	h.runDays("2026-01-01", 10) // kỳ 1: 164.000
	if h.accrued("S1") != 164_000 {
		t.Fatalf("kỳ 1 = %d, muốn 164000", h.accrued("S1"))
	}
	h.store.periods["S1"] = "2026-01-11" // op nhập gốc hiệu lực 11/01
	h.store.savings[0].PrincipalMinor = 101_000_000
	h.runDays("2026-01-11", 10)
	if got := h.accrued("S1") - 164_000; got != 166_000 { // 101tr × 10 × 6 /36500 = 166.027
		t.Fatalf("kỳ 2 = %d, muốn 166000", got)
	}
}

// Gửi thêm giữa kỳ: số dư đầu kỳ suy ngược từ số dư hiện tại trừ khoản gửi thêm.
func TestRunDailyTopUpMidPeriod(t *testing.T) {
	h := newDailyHarness(t)
	h.addProduct("P1", intp(1000), strp(interest.RoundHalfUp))
	h.addRate("P1", "2026-01-01", 6)
	h.addSavings("S1", "P1", "2026-01-01", 200_000_000)
	h.store.topUps["S1"] = []repository.TopUp{{Date: "2026-01-16", AmountMinor: 100_000_000}}
	h.runDays("2026-01-01", 31)
	if h.accrued("S1") != 773_000 { // 15 ngày × 100tr + 16 ngày × 200tr = 772.602,74
		t.Fatalf("tổng = %d, muốn 773000", h.accrued("S1"))
	}
}

// Lãi suất đổi giữa kỳ theo lịch sử dpm_interest_rates của bucket.
func TestRunDailyRateChangeMidPeriod(t *testing.T) {
	h := newDailyHarness(t)
	h.addProduct("P1", intp(1000), strp(interest.RoundHalfUp))
	h.addRate("P1", "2026-01-01", 6)
	h.addRate("P1", "2026-07-15", 5)
	h.addSavings("S1", "P1", "2026-07-01", 100_000_000)
	h.runDays("2026-07-01", 30)
	if h.accrued("S1") != 449_000 {
		t.Fatalf("tổng = %d, muốn 449000", h.accrued("S1"))
	}
}

// EOD bỏ lỡ nhiều ngày: một dòng phủ cả cửa sổ, số tiền bằng lãi lũy kế làm tròn.
func TestRunDailyCatchesUpMissedDays(t *testing.T) {
	h := newDailyHarness(t)
	h.addProduct("P1", intp(1000), strp(interest.RoundHalfUp))
	h.addRate("P1", "2026-01-01", 6)
	h.addSavings("S1", "P1", "2026-01-01", 100_000_000)
	h.mustRun("2026-01-05") // 5 ngày × 16.438,356 = 82.191,78
	rows := h.store.accruals
	if len(rows) != 1 || rows[0].AmountMinor != 82_000 || rows[0].Days != 5 || rows[0].PeriodFrom != "2026-01-01" {
		t.Fatalf("dòng = %+v, muốn 1 dòng 82000 / 5 ngày từ 01/01", rows)
	}
}

// Sản phẩm thiếu round_no: bỏ qua sổ đó, các sổ khác vẫn dự chi; cuối lần chạy báo lỗi tổng hợp có mã.
func TestRunDailyMissingRoundConfigSkipsOnlyThatSavings(t *testing.T) {
	h := newDailyHarness(t)
	h.addProduct("OK", intp(1000), strp(interest.RoundHalfUp))
	h.addProduct("NOROUND", nil, nil)
	h.addRate("", "2026-01-01", 6)
	h.addSavings("S-NOROUND", "NOROUND", "2026-01-01", 100_000_000)
	h.addSavings("S-OK", "OK", "2026-01-01", 100_000_000)
	posted, err := h.run("2026-01-03")
	var typed *ardaerrors.Error
	if !errors.As(err, &typed) || typed.Code != CodeAccrualRoundConfigMissing {
		t.Fatalf("lỗi = %v, muốn mã %s", err, CodeAccrualRoundConfigMissing)
	}
	if !strings.Contains(typed.Fields["savings_codes"], "S-NOROUND") || strings.Contains(typed.Fields["savings_codes"], "S-OK") {
		t.Fatalf("savings_codes = %q, muốn chỉ S-NOROUND", typed.Fields["savings_codes"])
	}
	if posted != 1 || len(h.amounts("S-OK")) != 1 || len(h.amounts("S-NOROUND")) != 0 {
		t.Fatalf("posted = %d; S-OK %v; S-NOROUND %v", posted, h.amounts("S-OK"), h.amounts("S-NOROUND"))
	}
	// Sản phẩm không tồn tại cũng là thiếu cấu hình, không làm dừng lần chạy.
	h.addSavings("S-GONE", "MISSING", "2026-01-01", 1_000_000)
	if _, err := h.run("2026-01-04"); err == nil {
		t.Fatal("sản phẩm không tồn tại phải báo lỗi tổng hợp")
	}
}
