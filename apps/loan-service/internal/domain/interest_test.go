package domain

// Bảng case chuẩn của bộ tính lãi khoản vay (T9.1). Số liệu lấy từ các ca oracle EPAS trong
// difftest/lnm/CLUSTERS.md (cùng ví dụ B-DAY0001, B-BAL0012, B-BAL0023) và tính tay bằng số thập phân chính xác:
// lãi đoạn = dư nợ × lãi suất% × số ngày / (100 × cơ sở năm), một phép chia ở scale 10.
// Mã quyết định: G2-LNM-LAI-01 (cơ sở năm cấu hình, tách quá hạn, Decimal), G2-LNM-LAI-08 (cờ ngày CM124),
// P3-1 (làm tròn một lần theo ROUND_NO/CM163), P3-2 (dư nợ 0 không sinh lãi).

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func dd(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func decp(s string) *decimal.Decimal { d := dec(s); return &d }

func bal(y int, m time.Month, d int, amount string) BalanceEntry {
	return BalanceEntry{ValueDate: dd(y, m, d), Balance: dec(amount)}
}

func baseInput() InterestInput {
	return InterestInput{
		From:              dd(2026, 1, 1),
		To:                dd(2026, 1, 31),
		DateMethod:        DateMethodSkipLastDay,
		BaseCode:          "CSTL_02",
		Balances:          []BalanceEntry{bal(2026, 1, 1, "1000000000")},
		AgreementRate:     decp("8.5"),
		AgreementOverRate: decp("11.05"),
	}
}

func mustInterest(t *testing.T, in InterestInput) InterestResult {
	t.Helper()
	res, err := CalculateInterest(in)
	if err != nil {
		t.Fatalf("CalculateInterest: %v", err)
	}
	return res
}

func wantRaw(t *testing.T, res InterestResult, want string) {
	t.Helper()
	if !res.Raw.Equal(dec(want)) {
		t.Fatalf("lãi thô = %s, muốn %s", res.Raw, want)
	}
}

func TestInterestBaseDays(t *testing.T) {
	got, err := InterestBaseDays("CSTL_02")
	if err != nil || got != 365 {
		t.Fatalf("InterestBaseDays(CSTL_02) = %d, %v; muốn 365", got, err)
	}
}

// G2-LNM-LAI-01: CSTL_01 chưa có định nghĩa, mã lạ hoặc để trống không được đoán.
func TestInterestBaseCodeRejectedWithCode(t *testing.T) {
	// Danh mục EPAS chỉ có CSTL_01 và CSTL_02; mọi mã khác (kể cả 360/365/366 trần, CSTL_360, sai hoa thường, thừa
	// khoảng trắng) không nằm trong danh sách đã xác nhận nên bị từ chối, không đoán.
	for code, wantCode := range map[string]string{
		"CSTL_01":  ErrCodeInterestBaseUndefined,
		"":         ErrCodeInterestBaseUnknown,
		"CSTL_99":  ErrCodeInterestBaseUnknown,
		"ACT/365":  ErrCodeInterestBaseUnknown,
		"CSTL_360": ErrCodeInterestBaseUnknown,
		"CSTL_365": ErrCodeInterestBaseUnknown,
		"CSTL_366": ErrCodeInterestBaseUnknown,
		"360":      ErrCodeInterestBaseUnknown,
		"365":      ErrCodeInterestBaseUnknown,
		"366":      ErrCodeInterestBaseUnknown,
		"cstl_02":  ErrCodeInterestBaseUnknown,
		" CSTL_02": ErrCodeInterestBaseUnknown,
		"CSTL_02 ": ErrCodeInterestBaseUnknown,
	} {
		in := baseInput()
		in.BaseCode = code
		_, err := CalculateInterest(in)
		var ie *InterestError
		if !errors.As(err, &ie) || ie.Code != wantCode {
			t.Fatalf("mã cơ sở %q: lỗi = %v, muốn mã %s", code, err, wantCode)
		}
	}
}

// Ví dụ B-DAY0001: kỳ đầu CM124.002 tính cả ngày giải ngân, 1 ngày, cơ sở 365 (không phải 30/360).
func TestInterestFirstDayCountsDisbursementDay(t *testing.T) {
	in := baseInput()
	in.From, in.To = dd(2026, 1, 31), dd(2026, 1, 31)
	res := mustInterest(t, in)
	if res.Days != 1 {
		t.Fatalf("số ngày = %d, muốn 1", res.Days)
	}
	wantRaw(t, res, "232876.7123287671")
}

func TestInterestFromDay(t *testing.T) {
	disburse := dd(2026, 3, 10)
	later := dd(2026, 4, 1)
	tests := []struct {
		name   string
		base   *time.Time
		method string
		want   time.Time
	}{
		{"kỳ đầu CM124.002: tính từ ngày giải ngân", nil, DateMethodSkipLastDay, dd(2026, 3, 10)},
		{"mốc đúng ngày giải ngân CM124.002", &disburse, DateMethodSkipLastDay, dd(2026, 3, 10)},
		{"kỳ sau CM124.002: mốc + 1", &later, DateMethodSkipLastDay, dd(2026, 4, 2)},
		{"kỳ đầu CM124.001: ngày sau giải ngân", nil, DateMethodSkipFirstDay, dd(2026, 3, 11)},
		{"kỳ sau CM124.001: mốc + 1", &later, DateMethodSkipFirstDay, dd(2026, 4, 2)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InterestFromDay(disburse, tc.base, tc.method)
			if err != nil || !got.Equal(tc.want) {
				t.Fatalf("InterestFromDay = %s, %v; muốn %s", got.Format("2006-01-02"), err, tc.want.Format("2006-01-02"))
			}
		})
	}
}

// Cờ ngày và kiểu làm tròn khớp CHÍNH XÁC theo mã; mã lạ hoặc rỗng là lỗi có mã, không im lặng thành một chế độ.
func TestInterestRejectsUnsupportedDateMethod(t *testing.T) {
	for _, method := range []string{"", "CM124.003", "CM124.001,CM124.002", "CM124.002X", "xCM124.001", "cm124.002", " CM124.002", "CM124"} {
		in := baseInput()
		in.DateMethod = method
		_, err := CalculateInterest(in)
		if !hasInterestCode(err, ErrCodeInterestDateMethodUnsupported) {
			t.Fatalf("cờ ngày %q: lỗi = %v, muốn mã %s", method, err, ErrCodeInterestDateMethodUnsupported)
		}
	}
	if _, err := CalculateInterest(baseInput()); err != nil {
		t.Fatalf("CM124.002 hợp lệ bị từ chối: %v", err)
	}
	in := baseInput()
	in.DateMethod = DateMethodSkipFirstDay
	if _, err := CalculateInterest(in); err != nil {
		t.Fatalf("CM124.001 hợp lệ bị từ chối: %v", err)
	}
}

func TestInterestRejectsUnsupportedRoundType(t *testing.T) {
	for _, kind := range []string{"", "ROUND", "CEIL", "FLOOR", "CEILING", "HALF_UP", "CM163.999", "CM163", "cm163.001", " CM163.001", "CM163.001 "} {
		in := baseInput()
		in.RoundNo, in.RoundType = 1000, kind
		if _, err := CalculateInterest(in); !hasInterestCode(err, ErrCodeInterestRoundTypeUnsupported) {
			t.Fatalf("kiểu làm tròn %q: lỗi = %v, muốn mã %s", kind, err, ErrCodeInterestRoundTypeUnsupported)
		}
		if _, err := RoundInterest(dec("1500"), 1000, kind); !hasInterestCode(err, ErrCodeInterestRoundTypeUnsupported) {
			t.Fatalf("RoundInterest kiểu %q: lỗi = %v", kind, err)
		}
	}
	// Không làm tròn theo bước (ROUND_NO <= 0) thì kiểu không có nghĩa và không bị kiểm.
	in := baseInput()
	in.RoundNo, in.RoundType = 0, ""
	if _, err := CalculateInterest(in); err != nil {
		t.Fatalf("ROUND_NO 0 không cần kiểu: %v", err)
	}
}

func TestInterestFromDayRejectsUnsupportedDateMethod(t *testing.T) {
	for _, method := range []string{"", "CM124.003", "CM124.001,CM124.002", "cm124.002"} {
		if _, err := InterestFromDay(dd(2026, 3, 10), nil, method); !hasInterestCode(err, ErrCodeInterestDateMethodUnsupported) {
			t.Fatalf("cờ ngày %q: lỗi = %v", method, err)
		}
	}
}

// Tất toán CM124.002 bỏ ngày cuối: 30 ngày thay vì 31.
func TestInterestSettlementDropsLastDay(t *testing.T) {
	in := baseInput()
	res := mustInterest(t, in)
	if res.Days != 31 {
		t.Fatalf("kỳ thường %d ngày, muốn 31", res.Days)
	}
	wantRaw(t, res, "7219178.0821917808")
	in.IsSettlement = true
	res = mustInterest(t, in)
	if res.Days != 30 {
		t.Fatalf("tất toán %d ngày, muốn 30", res.Days)
	}
	wantRaw(t, res, "6986301.3698630137")
	// CM124.001 không bỏ ngày cuối khi tất toán.
	in.DateMethod = DateMethodSkipFirstDay
	if res = mustInterest(t, in); res.Days != 31 {
		t.Fatalf("CM124.001 tất toán %d ngày, muốn 31", res.Days)
	}
}

// Ví dụ B-BAL0023 (CM124.001): thay đổi số dư ngày D có hiệu lực từ D+1.
func TestInterestSkipFirstDayShiftsChangesByOneDay(t *testing.T) {
	in := baseInput()
	in.From, in.To = dd(2026, 3, 16), dd(2026, 4, 29) // kỳ đầu CM124.001 bắt đầu sau ngày giải ngân 15/03
	in.DateMethod = DateMethodSkipFirstDay
	in.Balances = []BalanceEntry{bal(2026, 3, 15, "500000000"), bal(2026, 3, 25, "900000000")}
	res := mustInterest(t, in)
	// 500 triệu × 10 ngày (16/03-25/03) + 900 triệu × 35 ngày (26/03-29/04)
	wantRaw(t, res, "8500000")
	if len(res.Segments) != 2 || res.Segments[0].Days != 10 || res.Segments[1].Days != 35 {
		t.Fatalf("đoạn = %+v, muốn 10 + 35 ngày", res.Segments)
	}
	// CM124.002: thay đổi ngày D có hiệu lực ngay từ D.
	in.DateMethod = DateMethodSkipLastDay
	in.From = dd(2026, 3, 15)
	res = mustInterest(t, in)
	if len(res.Segments) != 2 || res.Segments[0].Days != 10 || res.Segments[1].Days != 36 {
		t.Fatalf("CM124.002 đoạn = %+v, muốn 10 + 36 ngày", res.Segments)
	}
}

// Ví dụ B-BAL0012: hoàn tất giải ngân trễ, các ngày trước khi có dư nợ không sinh lãi và không bị gom vào lần sau.
func TestInterestNoInterestBeforeDisbursementCompletes(t *testing.T) {
	in := baseInput()
	in.From, in.To = dd(2026, 3, 15), dd(2026, 4, 29)
	in.Balances = []BalanceEntry{bal(2026, 3, 16, "900000000")}
	res := mustInterest(t, in)
	if res.Days != 45 {
		t.Fatalf("số ngày có dư nợ = %d, muốn 45 (16/03-29/04)", res.Days)
	}
	wantRaw(t, res, "9431506.8493150685")
}

// P3-2: dư nợ về 0 rồi có lại, ngày dư nợ 0 không sinh lãi.
func TestInterestZeroBalanceGapAccruesNothing(t *testing.T) {
	in := baseInput()
	in.Balances = []BalanceEntry{
		bal(2026, 1, 1, "1000000000"), bal(2026, 1, 11, "0"), bal(2026, 1, 21, "1000000000"),
	}
	res := mustInterest(t, in)
	if res.Days != 21 || len(res.Segments) != 2 {
		t.Fatalf("ngày = %d, đoạn = %+v; muốn 21 ngày trong 2 đoạn (10 + 11)", res.Days, res.Segments)
	}
	wantRaw(t, res, "4890410.9589041096")
}

// Lãi suất đổi giữa kỳ: lãi suất theo từng đoạn, không dùng lãi suất ngày chạy cho cả kỳ.
func TestInterestRateHistorySplitsSegments(t *testing.T) {
	in := baseInput()
	in.Rates = []RateEntry{
		{EffectiveDate: dd(2025, 12, 1), Rate: decp("8.5"), OverRate: decp("11.05")},
		{EffectiveDate: dd(2026, 1, 15), Rate: decp("9.5"), OverRate: decp("12.35")},
	}
	res := mustInterest(t, in)
	if len(res.Segments) != 2 || res.Segments[0].Days != 14 || res.Segments[1].Days != 17 {
		t.Fatalf("đoạn = %+v, muốn 14 + 17 ngày", res.Segments)
	}
	wantRaw(t, res, "7684931.506849315")
}

// Quá hạn: phần dư nợ quá hạn tính theo lãi suất quá hạn, tách khỏi lãi trong hạn.
func TestInterestOverdueSplit(t *testing.T) {
	in := baseInput()
	in.Overdue = &OverdueRule{Date: dd(2026, 1, 21)}
	res := mustInterest(t, in)
	wantRaw(t, res, "7987671.2328767124")
	if !res.Normal.Equal(dec("4657534.2465753425")) || !res.Overdue.Equal(dec("3330136.9863013699")) {
		t.Fatalf("trong hạn %s / quá hạn %s", res.Normal, res.Overdue)
	}
	// ngày ân hạn gốc dời mốc quá hạn, gia hạn thì không cộng ân hạn
	in.Overdue = &OverdueRule{Date: dd(2026, 1, 11), GraceDays: 10}
	wantRaw(t, mustInterest(t, in), "7987671.2328767124")
	in.Overdue = &OverdueRule{Date: dd(2026, 1, 21), GraceDays: 10, Extension: true}
	wantRaw(t, mustInterest(t, in), "7987671.2328767124")
}

func TestInterestErrors(t *testing.T) {
	in := baseInput()
	in.Balances = nil
	if _, err := CalculateInterest(in); !hasInterestCode(err, ErrCodeInterestBalanceMissing) {
		t.Fatalf("thiếu lịch sử số dư: %v", err)
	}
	in = baseInput()
	in.AgreementOverRate = nil
	in.Overdue = &OverdueRule{Date: dd(2026, 1, 21)}
	if _, err := CalculateInterest(in); !hasInterestCode(err, ErrCodeInterestOverRateMissing) {
		t.Fatalf("quá hạn thiếu lãi suất quá hạn: %v", err)
	}
}

func hasInterestCode(err error, code string) bool {
	var ie *InterestError
	return errors.As(err, &ie) && ie.Code == code
}

// P3-1: làm tròn MỘT lần theo ROUND_NO với kiểu CM163; mỗi phần (trong hạn, quá hạn) và tổng đều làm tròn đúng một lần.
func TestInterestRoundsOnceByRoundNo(t *testing.T) {
	in := baseInput()
	in.Overdue = &OverdueRule{Date: dd(2026, 1, 21)}
	in.RoundNo, in.RoundType = 1000, RoundModeHalfUp
	res := mustInterest(t, in)
	// thô 7.987.671,23 → bội 1.000 HALF_UP = 7.988.000; phần: 4.657.534,25 → 4.658.000 và 3.330.136,99 → 3.330.000
	if !res.Total.Equal(dec("7988000")) {
		t.Fatalf("tổng = %s, muốn 7988000", res.Total)
	}
	if !res.NormalRounded.Equal(dec("4658000")) || !res.OverdueRounded.Equal(dec("3330000")) {
		t.Fatalf("phần trong hạn %s / quá hạn %s", res.NormalRounded, res.OverdueRounded)
	}
	in.RoundType = RoundModeCeiling
	if got := mustInterest(t, in).Total; !got.Equal(dec("7988000")) {
		t.Fatalf("CEILING = %s, muốn 7988000", got)
	}
	in.RoundType = RoundModeFloor
	if got := mustInterest(t, in).Total; !got.Equal(dec("7987000")) {
		t.Fatalf("FLOOR = %s, muốn 7987000", got)
	}
	in.RoundNo = 0
	if got := mustInterest(t, in).Total; !got.Equal(res.Raw) {
		t.Fatalf("ROUND_NO 0 phải giữ nguyên lãi thô, nhận %s", got)
	}
}

// Kiểu làm tròn đọc đúng mã CM163 và tên CEIL/FLOOR; không chép lỗi EPAS (mã CM163.002/.003 thành HALF_UP) và lỗi làm
// tròn kép (chia scale 10 trước khi Ceil/Floor/Round).
func TestRoundInterestHonoursCM163AndIsExact(t *testing.T) {
	tests := []struct {
		amount    string
		roundNo   int
		roundType string
		want      string
	}{
		{"1293759.5", 1, RoundModeFloor, "1293759"}, // EPAS ra 1.293.760 vì mã CM163.003 thành HALF_UP
		{"1293759.5", 1, RoundModeCeiling, "1293760"},
		{"1293759.5", 1, RoundModeHalfUp, "1293760"},
		{"1499.9999999999", 1000, RoundModeHalfUp, "1000"}, // EPAS làm tròn kép ra 2.000
		{"1499.9999999999", 10, RoundModeFloor, "1490"},
		{"5000.0000000001", 1000, RoundModeCeiling, "6000"},
		{"4999.9999999999", 1000, RoundModeFloor, "4000"},
		{"-12345.5", 1, RoundModeHalfUp, "-12346"}, // nửa ra xa số 0
		{"500", 1000, RoundModeHalfUp, "1000"},
		{"499.9999999999", 1000, RoundModeHalfUp, "0"},
		{"12.5", 0, RoundModeCeiling, "12.5"}, // ROUND_NO 0 giữ nguyên
		{"12.5", -3, RoundModeCeiling, "12.5"},
		{"12.5", 0, "", "12.5"}, // ROUND_NO <= 0: kiểu không có nghĩa
		{"2500", 1000, RoundModeHalfUp, "3000"},
	}
	for _, tc := range tests {
		got, err := RoundInterest(dec(tc.amount), tc.roundNo, tc.roundType)
		if err != nil {
			t.Fatalf("RoundInterest(%s, %d, %q): %v", tc.amount, tc.roundNo, tc.roundType, err)
		}
		if !got.Equal(dec(tc.want)) {
			t.Fatalf("RoundInterest(%s, %d, %q) = %s, muốn %s", tc.amount, tc.roundNo, tc.roundType, got, tc.want)
		}
	}
}

// Kết quả không phụ thuộc múi giờ của người gọi (lỗi E6 của EPAS: múi giờ có DST làm mất hoặc thừa ngày).
func TestInterestIgnoresCallerTimeZone(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("không có dữ liệu múi giờ")
	}
	in := baseInput()
	in.From, in.To = time.Date(2026, 3, 1, 0, 0, 0, 0, ny), time.Date(2026, 3, 31, 0, 0, 0, 0, ny) // qua ngày đổi giờ 08/03
	in.Balances = []BalanceEntry{{ValueDate: time.Date(2026, 3, 1, 0, 0, 0, 0, ny), Balance: dec("1000000000")}}
	res := mustInterest(t, in)
	if res.Days != 31 {
		t.Fatalf("số ngày = %d, muốn 31", res.Days)
	}
}
