package service

// Bảng case chuẩn — số học số dư hai pha (accounting-posting-contract.md
// §2.1, balance_math.go): khả dụng = đầu kỳ + đã post + đang giữ (theo chiều
// tự nhiên của tài khoản); dòng xuất (delta âm) lúc reserve phải nằm trong
// khả dụng (BAL_AVAILABLE_IS_NOT_ENOUGH), lúc post phải nằm trong thực tế =
// đầu kỳ + đã post (BAL_ACTUAL_IS_NOT_ENOUGH); post không đổi khả dụng (hold
// chuyển sang posted). Tài khoản nature B (ngoại bảng) miễn kiểm tra.
// Số tiền là minor unit VND.

import (
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
)

func goldenRow(postedDebit, postedCredit, reservedDebit, reservedCredit int64) repository.BalanceRow {
	return repository.BalanceRow{
		Key:                 repository.BalanceKey{CoaVersion: "V1", AccountCode: "1131", CurrencyCode: "VND"},
		PostedDebitMinor:    postedDebit,
		PostedCreditMinor:   postedCredit,
		ReservedDebitMinor:  reservedDebit,
		ReservedCreditMinor: reservedCredit,
	}
}

func TestGoldenEffectiveAvailable(t *testing.T) {
	cases := []struct {
		name          string
		row           repository.BalanceRow
		opening       int64
		nature        string
		wantAvailable int64
		wantActual    int64
	}{
		{"TK tiền (D): đầu kỳ 1.000, chưa phát sinh → khả dụng 1.000", goldenRow(0, 0, 0, 0), 1_000, "D", 1_000, 1_000},
		{"TK tiền (D): post Nợ 500 / Có 200 → thực tế 1.300", goldenRow(500, 200, 0, 0), 1_000, "D", 1_300, 1_300},
		{"TK tiền (D): giữ Có 300 (đề nghị chi) → khả dụng 700, thực tế 1.000", goldenRow(0, 0, 0, 300), 1_000, "D", 700, 1_000},
		{"TK tiền (D): giữ Nợ 300 (đề nghị thu) làm tăng khả dụng", goldenRow(0, 0, 300, 0), 1_000, "D", 1_300, 1_000},
		{"TK tiền gửi KH (C): post Có 700 → nắm giữ 700", goldenRow(0, 700, 0, 0), 0, "C", 700, 700},
		{"TK (C): giữ Nợ 200 (đề nghị rút) → khả dụng 500", goldenRow(0, 700, 200, 0), 0, "C", 500, 700},
		{"TK (C) với đầu kỳ bên Có 1.000 + post Nợ 400 → 600", goldenRow(400, 0, 0, 0), 1_000, "C", 600, 600},
		{"TK (D) đầu kỳ âm (đầu kỳ bên Có) → khả dụng âm", goldenRow(0, 0, 0, 0), -100, "D", -100, -100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveAvailable(tc.row, tc.opening, tc.nature); got != tc.wantAvailable {
				t.Fatalf("khả dụng = %d, muốn %d", got, tc.wantAvailable)
			}
			if got := tc.opening + naturalSignedPosted(tc.row, tc.nature); got != tc.wantActual {
				t.Fatalf("thực tế = %d, muốn %d", got, tc.wantActual)
			}
		})
	}
}

func TestGoldenNaturalSignedOpening(t *testing.T) {
	cases := []struct {
		name      string
		direction string
		amount    int64
		nature    string
		want      int64
	}{
		{"đầu kỳ Nợ 100 trên TK D → +100", "DEBIT", 100, "D", 100},
		{"đầu kỳ Có 100 trên TK D → −100", "CREDIT", 100, "D", -100},
		{"đầu kỳ Có 100 trên TK C → +100", "CREDIT", 100, "C", 100},
		{"đầu kỳ Nợ 100 trên TK C → −100", "DEBIT", 100, "C", -100},
		{"đầu kỳ 0 → 0", "DEBIT", 0, "D", 0},
		{"đầu kỳ âm bị coi như không có", "DEBIT", -5, "D", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := naturalSignedOpening(repository.OpeningSide{Direction: tc.direction, AmountMinor: tc.amount}, tc.nature)
			if got != tc.want {
				t.Fatalf("= %d, muốn %d", got, tc.want)
			}
		})
	}
}

func TestGoldenReserveAndPostChecks(t *testing.T) {
	cases := []struct {
		name        string
		row         repository.BalanceRow
		opening     int64
		nature      string
		direction   string
		amount      int64
		wantReserve string // "" = qua, hoặc tiền tố mã lỗi
		wantPost    string
	}{
		{"chi 500 từ TK tiền 1.000: reserve và post đều qua", goldenRow(0, 0, 0, 0), 1_000, "D", "CREDIT", 500, "", ""},
		{"chi đúng 1.000 (vừa hết): qua", goldenRow(0, 0, 0, 0), 1_000, "D", "CREDIT", 1_000, "", ""},
		{"chi 1.001 vượt 1 đồng: BAL_AVAILABLE và BAL_ACTUAL", goldenRow(0, 0, 0, 0), 1_000, "D", "CREDIT", 1_001, "BAL_AVAILABLE_IS_NOT_ENOUGH", "BAL_ACTUAL_IS_NOT_ENOUGH"},
		{"đã giữ 800, chi thêm 300: reserve fail (khả dụng 200) nhưng actual còn 1.000 → post qua", goldenRow(0, 0, 0, 800), 1_000, "D", "CREDIT", 300, "BAL_AVAILABLE_IS_NOT_ENOUGH", ""},
		{"tiền đã rời qua đường khác (post Có 900): hold 800 không còn thực tế → post fail", goldenRow(0, 900, 0, 800), 1_000, "D", "CREDIT", 800, "BAL_AVAILABLE_IS_NOT_ENOUGH", "BAL_ACTUAL_IS_NOT_ENOUGH"},
		{"thu tiền (Nợ TK D) luôn tự do dù số dư 0", goldenRow(0, 0, 0, 0), 0, "D", "DEBIT", 9_999, "", ""},
		{"rút 700 từ tiền gửi KH (C) đang 700: qua", goldenRow(0, 700, 0, 0), 0, "C", "DEBIT", 700, "", ""},
		{"rút 701 từ tiền gửi KH 700: fail", goldenRow(0, 700, 0, 0), 0, "C", "DEBIT", 701, "BAL_AVAILABLE_IS_NOT_ENOUGH", "BAL_ACTUAL_IS_NOT_ENOUGH"},
		{"nộp thêm (Có TK C) luôn tự do", goldenRow(0, 0, 0, 0), 0, "C", "CREDIT", 9_999, "", ""},
		{"ngoại bảng (B) miễn kiểm tra cả hai pha", goldenRow(0, 0, 0, 0), 0, "B", "CREDIT", 9_999, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := func(label string, err error, want string) {
				t.Helper()
				if want == "" && err != nil {
					t.Fatalf("%s: muốn qua, nhận %v", label, err)
				}
				if want != "" && (err == nil || !strings.HasPrefix(err.Error(), want+":1131")) {
					t.Fatalf("%s: muốn %s:1131, nhận %v", label, want, err)
				}
			}
			check("reserve", checkReserve(tc.row, tc.opening, tc.nature, tc.direction, tc.amount), tc.wantReserve)
			check("post", checkPostActual(tc.row, tc.opening, tc.nature, tc.direction, tc.amount), tc.wantPost)
		})
	}
}

// Vòng đời counter: reserve → post giữ nguyên khả dụng; reserve → release
// trả lại đúng số đã giữ; direct post chỉ chạm posted.
func TestGoldenBalanceCounterLifecycle(t *testing.T) {
	const opening = 1_000
	row := goldenRow(0, 0, 0, 0)
	applyReserve(&row, "CREDIT", 300)
	if row.ReservedCreditMinor != 300 || effectiveAvailable(row, opening, "D") != 700 {
		t.Fatalf("sau reserve: %+v khả dụng %d", row, effectiveAvailable(row, opening, "D"))
	}
	applyPost(&row, "CREDIT", 300)
	if row.ReservedCreditMinor != 0 || row.PostedCreditMinor != 300 || effectiveAvailable(row, opening, "D") != 700 {
		t.Fatalf("sau post: %+v khả dụng %d (phải giữ 700)", row, effectiveAvailable(row, opening, "D"))
	}
	applyReserve(&row, "CREDIT", 200)
	applyRelease(&row, "CREDIT", 200)
	if row.ReservedCreditMinor != 0 || effectiveAvailable(row, opening, "D") != 700 {
		t.Fatalf("sau release: %+v", row)
	}
	applyDirectPost(&row, "DEBIT", 50)
	if row.PostedDebitMinor != 50 || row.ReservedDebitMinor != 0 || effectiveAvailable(row, opening, "D") != 750 {
		t.Fatalf("sau direct post: %+v", row)
	}
}
