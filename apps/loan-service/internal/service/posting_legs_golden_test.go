package service

// Bảng case chuẩn — bút toán loan-service tự dựng và gửi finance
// (LNM_ACCRUAL, LNM_PROVISION batch EOD, LNM_PROVISION chung 307,
// LNM_PROVISION_306 cụ thể).
//
// Mỗi case: số tiền đầu vào → các dòng PostingLine sau khi leg được giải qua
// rule card (financeclient.PostingLinesFromRules) — khóa chính xác số dòng,
// chiều Nợ/Có, số tiền và classification từng dòng, và cân Nợ = Có.
// Rule card lấy đúng theo seed finance-service/migrations
// (20260907090200_rule_cards_seed, 20260909100000_finance_provision_rule,
// 20260911141000_lnm_provision_306_card) và docs/accounting-rule-cards.md.
// Số tiền là minor unit VND.

import (
	"testing"

	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// goldenLoanCards mirrors the seeded fin_accounting_rules rows for the cards
// loan-service posts directly (keep in sync with the finance migrations).
var goldenLoanCards = map[string][]*financev1.PostingRule{
	"LNM_ACCRUAL": {
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_INTEREST_RECEIVABLE"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_INTEREST_INCOME"},
	},
	"LNM_PROVISION": {
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_PROVISION_EXPENSE"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_PROVISION_LIABILITY"},
		{LineNo: 3, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_PROVISION_LIABILITY"},
		{LineNo: 4, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_PROVISION_RELEASE"},
	},
	"LNM_PROVISION_306": {
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_PROVISION_EXPENSE"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_PROVISION_LIABILITY"},
		{LineNo: 3, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_PROVISION_LIABILITY"},
		{LineNo: 4, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_PROVISION_RELEASE"},
	},
}

// goldenLine is one expected resolved posting line.
type goldenLine struct {
	direction      string
	amount         int64
	classification string
}

func assertGoldenLines(t *testing.T, lines []*financev1.PostingLine, want []goldenLine) {
	t.Helper()
	if len(lines) != len(want) {
		t.Fatalf("số dòng = %d, muốn %d: %+v", len(lines), len(want), lines)
	}
	var debit, credit int64
	for i, w := range want {
		got := lines[i]
		if got.GetLineNo() != int32(i+1) {
			t.Fatalf("dòng %d có line_no %d (phải đánh số liên tục từ 1)", i+1, got.GetLineNo())
		}
		if got.GetDirection() != w.direction || got.GetAmountMinor() != w.amount ||
			got.GetAnalytics().GetAccClassification() != w.classification {
			t.Fatalf("dòng %d = %s %d %s, muốn %s %d %s", i+1,
				got.GetDirection(), got.GetAmountMinor(), got.GetAnalytics().GetAccClassification(),
				w.direction, w.amount, w.classification)
		}
		if got.GetAmountMinor() <= 0 {
			t.Fatalf("dòng %d có số tiền không dương: %d", i+1, got.GetAmountMinor())
		}
		switch got.GetDirection() {
		case "DEBIT":
			debit += got.GetAmountMinor()
		case "CREDIT":
			credit += got.GetAmountMinor()
		}
	}
	if debit != credit {
		t.Fatalf("bút toán không cân: Nợ %d ≠ Có %d", debit, credit)
	}
}

var goldenAgreement = repository.AccruableAgreement{
	ID: "agr-1", ContractCode: "HD-001", AgreementCode: "KU-001",
	DebtGroupCode: "GROUP_1", AccClassification: "PGD01", CurrencyCode: "VND",
}

func TestGoldenAccrualPostingLines(t *testing.T) {
	cases := []struct {
		name     string
		interest int64
		want     []goldenLine
	}{
		{"lãi 10 triệu: Nợ phải thu lãi / Có doanh thu lãi", 10_000_000, []goldenLine{
			{"DEBIT", 10_000_000, "LNM_INTEREST_RECEIVABLE"},
			{"CREDIT", 10_000_000, "LNM_INTEREST_INCOME"},
		}},
		{"lãi 1 đồng vẫn đủ 2 dòng", 1, []goldenLine{
			{"DEBIT", 1, "LNM_INTEREST_RECEIVABLE"},
			{"CREDIT", 1, "LNM_INTEREST_INCOME"},
		}},
		{"lãi 0: không có dòng nào (service bỏ qua, không gọi finance)", 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legs := accrualLegs(goldenAgreement, tc.interest)
			lines, err := financeclient.PostingLinesFromRules("LNM_ACCRUAL", goldenLoanCards["LNM_ACCRUAL"], legs, "VND")
			if err != nil {
				t.Fatalf("lỗi giải rule card: %v", err)
			}
			assertGoldenLines(t, lines, tc.want)
			for _, line := range lines {
				a := line.GetAnalytics()
				if a.GetContractCode() != "HD-001" || a.GetOrgUnitCode() != "PGD01" || a.GetDimensions()["agreement_code"] != "KU-001" {
					t.Fatalf("thiếu analytics hợp đồng/đơn vị/khế ước: %+v", a)
				}
			}
			if len(lines) == 2 && lines[0].GetAnalytics().GetDebtGroupCode() != "GROUP_1" {
				t.Fatalf("dòng Nợ phải mang nhóm nợ để resolve TK phải thu: %+v", lines[0].GetAnalytics())
			}
		})
	}
}

func TestGoldenEODProvisionPostingLines(t *testing.T) {
	cases := []struct {
		name  string
		delta int64
		want  []goldenLine
	}{
		{"delta dương 20 triệu → trích tăng: Nợ chi phí DP / Có DP phải trả (card 1-2)", 20_000_000, []goldenLine{
			{"DEBIT", 20_000_000, "LNM_PROVISION_EXPENSE"},
			{"CREDIT", 20_000_000, "LNM_PROVISION_LIABILITY"},
		}},
		{"delta âm 20 triệu → hoàn giảm: Nợ DP phải trả / Có hoàn nhập DP (card 3-4), số tiền dương", -20_000_000, []goldenLine{
			{"DEBIT", 20_000_000, "LNM_PROVISION_LIABILITY"},
			{"CREDIT", 20_000_000, "LNM_PROVISION_RELEASE"},
		}},
		{"delta 0: không có dòng nào", 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legs := provisionLegs(goldenAgreement, tc.delta)
			lines, err := financeclient.PostingLinesFromRules("LNM_PROVISION", goldenLoanCards["LNM_PROVISION"], legs, "VND")
			if err != nil {
				t.Fatalf("lỗi giải rule card: %v", err)
			}
			assertGoldenLines(t, lines, tc.want)
		})
	}
}

func TestGoldenGeneralProvisionPostingLines(t *testing.T) {
	cases := []struct {
		name           string
		alloc, reverse int64
		want           []goldenLine
	}{
		{"kỳ đầu trích 1,5 tỷ: chỉ cặp trích tăng", 1_500_000_000, 0, []goldenLine{
			{"DEBIT", 1_500_000_000, "LNM_PROVISION_EXPENSE"},
			{"CREDIT", 1_500_000_000, "LNM_PROVISION_LIABILITY"},
		}},
		{"kỳ sau dư nợ giảm, hoàn 300 triệu: chỉ cặp hoàn giảm", 0, 300_000_000, []goldenLine{
			{"DEBIT", 300_000_000, "LNM_PROVISION_LIABILITY"},
			{"CREDIT", 300_000_000, "LNM_PROVISION_RELEASE"},
		}},
		{"không chênh lệch: không có dòng nào (service bỏ qua posting)", 0, 0, nil},
		// GeneralProvisionDelta không bao giờ trả cả hai > 0, nhưng leg builder vẫn
		// xếp đúng thứ tự nếu nhận cả hai — khóa hành vi.
		{"cả trích thêm và hoàn (dữ liệu bất thường): 4 dòng, trích trước hoàn sau", 100, 40, []goldenLine{
			{"DEBIT", 100, "LNM_PROVISION_EXPENSE"},
			{"CREDIT", 100, "LNM_PROVISION_LIABILITY"},
			{"DEBIT", 40, "LNM_PROVISION_LIABILITY"},
			{"CREDIT", 40, "LNM_PROVISION_RELEASE"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legs := generalProvisionLegs("PGD01", tc.alloc, tc.reverse)
			lines, err := financeclient.PostingLinesFromRules("LNM_PROVISION", goldenLoanCards["LNM_PROVISION"], legs, "VND")
			if err != nil {
				t.Fatalf("lỗi giải rule card: %v", err)
			}
			assertGoldenLines(t, lines, tc.want)
			for _, line := range lines {
				if line.GetAnalytics().GetOrgUnitCode() != "PGD01" {
					t.Fatalf("dự phòng chung phải gắn đơn vị: %+v", line.GetAnalytics())
				}
			}
		})
	}
}

func TestGoldenSpecificProvisionPostingLines(t *testing.T) {
	analytics := &financev1.Analytics{ContractCode: "HD-001", DebtGroupCode: "GROUP_3"}
	cases := []struct {
		name  string
		delta int64
		want  []goldenLine
	}{
		{"306 trích thêm 15 triệu (BUG-5: theo delta, không post nguyên số phải có)", 15_000_000, []goldenLine{
			{"DEBIT", 15_000_000, "LNM_PROVISION_EXPENSE"},
			{"CREDIT", 15_000_000, "LNM_PROVISION_LIABILITY"},
		}},
		{"306 hoàn nhập 15 triệu", -15_000_000, []goldenLine{
			{"DEBIT", 15_000_000, "LNM_PROVISION_LIABILITY"},
			{"CREDIT", 15_000_000, "LNM_PROVISION_RELEASE"},
		}},
		{"306 không chênh lệch: không có dòng", 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legs := specificProvisionPostingLegs(tc.delta, analytics)
			lines, err := financeclient.PostingLinesFromRules("LNM_PROVISION_306", goldenLoanCards["LNM_PROVISION_306"], legs, "VND")
			if err != nil {
				t.Fatalf("lỗi giải rule card: %v", err)
			}
			assertGoldenLines(t, lines, tc.want)
		})
	}
}

// Rule card thiếu dòng → lỗi RULE_NOT_FOUND có kiểu, không có fallback hard-code
// (T3.3): leg hoàn giảm trên card chỉ có 2 dòng phải bị từ chối.
func TestGoldenProvisionReversalNeedsCardLines3And4(t *testing.T) {
	twoLineCard := goldenLoanCards["LNM_PROVISION"][:2]
	_, err := financeclient.PostingLinesFromRules("LNM_PROVISION", twoLineCard, provisionLegs(goldenAgreement, -1_000), "VND")
	typed, ok := financeclient.AsPostingError(err)
	if !ok || typed.Code != financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND {
		t.Fatalf("muốn RULE_NOT_FOUND, nhận %v", err)
	}
}
