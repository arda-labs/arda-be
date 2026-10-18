//go:build epasgolden

// Cầu nối giữa vector oracle EPAS (egCase) và bộ tính lãi sản xuất domain.CalculateInterest (T9.1).
// Phần dựng đầu vào (ngày bắt đầu theo calc_type, gộp lịch sử số dư, cờ quá hạn) là việc của tầng gọi, nên
// nằm ở đây; mọi quy tắc tính lãi (cơ sở năm, đếm ngày CM124, tách quá hạn, đoạn dư nợ 0, làm tròn ROUND_NO)
// do hàm sản xuất đảm nhiệm.
package domain

import (
	"math/big"
	"time"

	"github.com/shopspring/decimal"
)

// newOut là kết quả của bộ tính sản xuất cho một ca.
type newOut struct {
	Total   *big.Rat // tổng đã làm tròn ROUND_NO (chưa trừ lãi thừa thiếu / khoản đã thu)
	Raw     *big.Rat // tổng chưa làm tròn
	Days    int
	Err     string // mã lỗi của hàm sản xuất, hoặc "NOT_SUPPORTED:<lý do>" khi ca dùng tính năng Arda chưa có
	Skipped bool
}

func decOf(s *string) *decimal.Decimal {
	if s == nil {
		return nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil
	}
	return &d
}

func decOrZero(s *string) decimal.Decimal {
	if d := decOf(s); d != nil {
		return *d
	}
	return decimal.Zero
}

func dayTimeOf(n int) time.Time { return time.Unix(int64(n)*86400, 0).UTC() }

func ratFromDecimal(d decimal.Decimal) *big.Rat { return d.Rat() }

// newFromDay tính ngày bắt đầu kỳ tính lãi theo calc_type và cờ CM124 (domain.InterestFromDay).
func newFromDay(c *egCase) (time.Time, string) {
	D := dayTimeOf(dayNum(c.Agreement.DisburseDate))
	method := ""
	if c.Product.DateMethod != nil {
		method = *c.Product.DateMethod
	}
	// Mã cờ ngày không hỗ trợ: bộ tính sản xuất từ chối bằng mã lỗi.
	if _, err := InterestFromDay(D, nil, method); err != nil {
		return time.Time{}, "DATE_METHOD:" + err.(*InterestError).Code
	}
	from := func(base *int) time.Time {
		if base == nil {
			t, _ := InterestFromDay(D, nil, method)
			return t
		}
		b := dayTimeOf(*base)
		t, _ := InterestFromDay(D, &b, method)
		return t
	}
	if fo := optDay(c.Calc.FromDate); fo != nil {
		return dayTimeOf(*fo), ""
	}
	I := optDay(c.Agreement.InterestToDate)
	A := optDay(c.Agreement.AcrInterestDate)
	IA := optDay(c.Agreement.InterestAllocToDate)
	applyDiff := c.Param.ApplyInterestDiff != nil && *c.Param.ApplyInterestDiff == "1"
	switch c.CalcType {
	case "INT_ALLOC":
		if IA != nil {
			return dayTimeOf(*IA + 1), ""
		}
		return from(nil), ""
	case "INT_ACCR":
		if contains(c.Product.StartMethod, "CM125.002") {
			return from(nil), ""
		}
		var base int
		if applyDiff {
			if I == nil {
				return time.Time{}, "VALIDATION_ERROR"
			}
			base = *I
		} else {
			switch {
			case I == nil && A == nil:
				return time.Time{}, "VALIDATION_ERROR"
			case I == nil:
				base = *A
			case A == nil || *A < *I:
				base = *I
			default:
				base = *A
			}
		}
		return from(&base), ""
	default:
		if contains(c.Product.StartMethod, "CM125.001") && I != nil {
			return from(I), ""
		}
		return from(nil), ""
	}
}

// newCalc đưa một ca vào domain.CalculateInterest.
func newCalc(c *egCase) newOut {
	if contains(c.Product.CalcMethod, "CM123.002") {
		return newOut{Skipped: true, Err: "NOT_SUPPORTED:CM123.002"}
	}
	F, errCode := newFromDay(c)
	if errCode != "" {
		return newOut{Err: errCode}
	}
	T := dayNum(c.Calc.CalculateDate)
	in := InterestInput{
		From:              F,
		To:                dayTimeOf(T),
		IsSettlement:      c.Calc.IsSettlement,
		RoundNo:           0,
		AgreementRate:     decOf(c.Agreement.InterestRate),
		AgreementOverRate: decOf(c.Agreement.OverInterestRate),
	}
	if c.Product.DateMethod != nil {
		in.DateMethod = *c.Product.DateMethod
	}
	if c.Product.BaseCode != nil {
		in.BaseCode = *c.Product.BaseCode
	}
	if c.Product.RoundNo != nil {
		in.RoundNo = *c.Product.RoundNo
	}
	if c.Product.RoundType != nil {
		in.RoundType = fixtureRoundCode(*c.Product.RoundType)
	}
	for _, b := range mergeBalances(c.Balances, T) {
		in.Balances = append(in.Balances, BalanceEntry{ValueDate: dayTimeOf(b.day), Balance: decimal.NewFromBigRat(b.bal, 10)})
	}
	for _, r := range c.Rates {
		in.Rates = append(in.Rates, RateEntry{EffectiveDate: dayTimeOf(dayNum(r.EffectiveDate)), Rate: decOf(r.Rate), OverRate: decOf(r.OverRate)})
	}
	if contains(c.Product.OverdueCheck, "CM126.001") {
		od, ex := optDay(c.Agreement.OverdueDate), optDay(c.Agreement.ExtensionDate)
		var rule *OverdueRule
		if ex != nil && (od == nil || *ex > *od) {
			rule = &OverdueRule{Date: dayTimeOf(*ex), Extension: true}
		} else if od != nil {
			rule = &OverdueRule{Date: dayTimeOf(*od)}
			if c.Product.PrincipalGrace != nil && *c.Product.PrincipalGrace > 0 {
				rule.GraceDays = *c.Product.PrincipalGrace
			}
		}
		in.Overdue = rule
	}
	if pay := optRat(c.Calc.PrincipalPayment); pay != nil && pay.Sign() > 0 {
		pd := optDay(c.Calc.PaymentDate)
		if pd == nil {
			return newOut{Err: "NONDETERMINISTIC_PAYMENT_DATE"}
		}
		amt, _ := decimal.NewFromString(pay.FloatString(0))
		in.Payment = &PrincipalPayment{Date: dayTimeOf(*pd), Amount: amt}
	}
	res, err := CalculateInterest(in)
	if err != nil {
		ie, ok := err.(*InterestError)
		if !ok {
			return newOut{Err: err.Error()}
		}
		switch ie.Code {
		case ErrCodeInterestBalanceMissing:
			return newOut{Err: "BUSINESS_DATA_MISSING"}
		case ErrCodeInterestOverRateMissing:
			return newOut{Err: "PARAMETER_CONFIG_NOT_FOUND"}
		case ErrCodeInterestBaseUndefined, ErrCodeInterestBaseUnknown:
			return newOut{Err: "BASE_CODE:" + ie.Code}
		case ErrCodeInterestDateMethodUnsupported:
			return newOut{Err: "DATE_METHOD:" + ie.Code}
		case ErrCodeInterestRoundTypeUnsupported:
			return newOut{Err: "ROUND_TYPE:" + ie.Code}
		}
		return newOut{Err: ie.Code}
	}
	total := res.Total
	// Dự thu có tham số lãi thừa thiếu: EPAS làm tròn riêng phần trong hạn và quá hạn, cộng, trừ chênh lệch và dự thu
	// đã ghi (nhóm 1) hoặc lãi ngoại bảng (nhóm khác), rồi làm tròn lại. Phần trừ nằm ngoài phạm vi T9.1 nên chỉ lắp ở
	// đây để so với EPAS; hàm sản xuất chỉ cấp hai phần đã làm tròn.
	if c.CalcType == "INT_ACCR" && c.Param.ApplyInterestDiff != nil && *c.Param.ApplyInterestDiff == "1" {
		t := res.NormalRounded.Add(res.OverdueRounded).Sub(decOrZero(c.Agreement.InterestDiffAmt))
		if c.Agreement.DebtGroup != nil && *c.Agreement.DebtGroup == "GROUP1" {
			t = t.Sub(decOrZero(c.Agreement.AcrInterestAmt))
		} else {
			t = t.Sub(decOrZero(c.Agreement.OffBalDueInterestAmt))
		}
		total, _ = RoundInterest(decimal.Max(t, decimal.Zero), in.RoundNo, in.RoundType)
	}
	return newOut{Total: ratFromDecimal(total), Raw: ratFromDecimal(res.Raw), Days: res.Days}
}

func ratEqualWithin(a, b *big.Rat, tol *big.Rat) bool {
	d := new(big.Rat).Sub(a, b)
	return d.Abs(d).Cmp(tol) <= 0
}

// fixtureRoundCode đưa tên kiểu làm tròn mà front-end EPAS lưu (ROUND, CEIL, FLOOR) về mã CM163 mà bộ tính sản xuất
// chấp nhận; mã CM163.00x giữ nguyên, giá trị khác giữ nguyên để bị từ chối. Việc đổi tên-sang-mã là của dữ liệu
// kiểm thử, không nằm trong mã sản xuất.
func fixtureRoundCode(kind string) string {
	switch kind {
	case "ROUND":
		return RoundModeHalfUp
	case "CEIL":
		return RoundModeCeiling
	case "FLOOR":
		return RoundModeFloor
	}
	return kind
}

// isBaseCodeCase: ca dùng mã cơ sở năm ngoài danh sách đã xác nhận (CSTL_02), gồm CSTL_01, CSTL_360, "360", thiếu mã.
// Bộ tính sản xuất phải từ chối các ca này bằng mã lỗi, EPAS thì đoán 360/365/366 từ chuỗi.
func isBaseCodeCase(c *egCase) bool {
	return c.Product.BaseCode == nil || *c.Product.BaseCode != "CSTL_02"
}
