package worker

// Bảng case chuẩn — bút toán giải ngân / thu nợ do workflow worker dựng cho
// loan (LNM_DISB_REGISTER, LNM_DISB_COMPLETE, LNM_COLLECTION; đường đơn và
// đường batch).
//
// Mỗi case: số tiền trên chứng từ → các dòng PostingLine sau khi giải rule
// card (financeclient.PostingLinesFromRules) — khóa số dòng, chiều Nợ/Có, số
// tiền, classification và cân Nợ = Có. Rule card lấy theo seed
// finance-service/migrations/20260909092000_finance_posting_rules_wire.sql và
// docs/accounting-rule-cards.md (LNM_DISBURSEMENT, LNM_COLLECTION,
// LNM_DISBURSEMENT_COMPLETE). Số tiền là minor unit VND.

import (
	"testing"

	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	loanv1 "github.com/arda-labs/arda/libs/go/arda-proto/loan/v1"
)

var goldenLoanWorkerCards = map[string][]*financev1.PostingRule{
	"LNM_DISB_REGISTER": {
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_LOAN_PRINCIPAL"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "FUND_DISBURSEMENT_IN_TRANSIT"},
	},
	"LNM_DISB_COMPLETE": {
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "FUND_DISBURSEMENT_IN_TRANSIT"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "CASH_SETTLEMENT_ACCOUNT"},
	},
	"LNM_COLLECTION": {
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "CASH_SETTLEMENT_ACCOUNT"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_LOAN_PRINCIPAL"},
		{LineNo: 3, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "CASH_SETTLEMENT_ACCOUNT"},
		{LineNo: 4, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_INTEREST_RECEIVABLE"},
	},
}

type goldenWorkerLine struct {
	direction      string
	amount         int64
	classification string
}

func assertGoldenWorkerLines(t *testing.T, lines []*financev1.PostingLine, want []goldenWorkerLine) {
	t.Helper()
	if len(lines) != len(want) {
		t.Fatalf("số dòng = %d, muốn %d: %+v", len(lines), len(want), lines)
	}
	var debit, credit int64
	for i, w := range want {
		got := lines[i]
		if got.GetLineNo() != int32(i+1) || got.GetDirection() != w.direction || got.GetAmountMinor() != w.amount ||
			got.GetAnalytics().GetAccClassification() != w.classification {
			t.Fatalf("dòng %d = #%d %s %d %s, muốn #%d %s %d %s", i+1,
				got.GetLineNo(), got.GetDirection(), got.GetAmountMinor(), got.GetAnalytics().GetAccClassification(),
				i+1, w.direction, w.amount, w.classification)
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

func TestGoldenDisbursementLegs(t *testing.T) {
	detail := &loanv1.DisbursementPostingDetail{
		DisbursementId: "disb-1", ContractCode: "HD-001", AgreementCode: "KU-001", DisburseAmtMinor: 600_000_000,
		CurrencyCode: "VND", DebtGroupCode: "GROUP_1", OrgUnitCode: "PGD01", CustomerCode: "KH-1", FundSourceCode: "NV-TW",
	}
	cases := []struct {
		name string
		card string
		want []goldenWorkerLine
	}{
		{"REGISTER 600 triệu: Nợ cho vay KH / Có tiền đang chuyển", "LNM_DISB_REGISTER", []goldenWorkerLine{
			{"DEBIT", 600_000_000, "LNM_LOAN_PRINCIPAL"},
			{"CREDIT", 600_000_000, "FUND_DISBURSEMENT_IN_TRANSIT"},
		}},
		{"COMPLETE 600 triệu: đảo tiền đang chuyển / Có tiền thanh toán — không tăng cho vay", "LNM_DISB_COMPLETE", []goldenWorkerLine{
			{"DEBIT", 600_000_000, "FUND_DISBURSEMENT_IN_TRANSIT"},
			{"CREDIT", 600_000_000, "CASH_SETTLEMENT_ACCOUNT"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines, err := financeclient.PostingLinesFromRules(tc.card, goldenLoanWorkerCards[tc.card], disbursementLegs(detail), "VND")
			if err != nil {
				t.Fatalf("lỗi giải rule card: %v", err)
			}
			assertGoldenWorkerLines(t, lines, tc.want)
			debitA, creditA := lines[0].GetAnalytics(), lines[1].GetAnalytics()
			if debitA.GetDebtGroupCode() != "GROUP_1" || debitA.GetCustomerCode() != "KH-1" || debitA.GetDimensions()["agreement_code"] != "KU-001" {
				t.Fatalf("dòng Nợ thiếu nhóm nợ/khách hàng/khế ước: %+v", debitA)
			}
			if creditA.GetFundSourceCode() != "NV-TW" || creditA.GetContractCode() != "HD-001" {
				t.Fatalf("dòng Có thiếu nguồn vốn/hợp đồng: %+v", creditA)
			}
		})
	}
	t.Run("giải ngân 0 đồng (dòng đóng hợp đồng): không có dòng bút toán", func(t *testing.T) {
		zero := &loanv1.DisbursementPostingDetail{DisburseAmtMinor: 0}
		lines, err := financeclient.PostingLinesFromRules("LNM_DISB_COMPLETE", goldenLoanWorkerCards["LNM_DISB_COMPLETE"], disbursementLegs(zero), "VND")
		if err != nil || len(lines) != 0 {
			t.Fatalf("muốn 0 dòng, nhận %d (%v)", len(lines), err)
		}
	})
}

func TestGoldenCollectionLegs(t *testing.T) {
	base := func(principal, interest int64) *loanv1.CollectionPostingDetail {
		return &loanv1.CollectionPostingDetail{
			CollectionId: "col-1", ContractCode: "HD-001", AgreementCode: "KU-001", CollectionDate: "2026-09-07",
			PrincipalMinor: principal, InterestMinor: interest, CurrencyCode: "VND",
			DebtGroupCode: "GROUP_2", OrgUnitCode: "PGD01", CustomerCode: "KH-1",
		}
	}
	cases := []struct {
		name                string
		principal, interest int64
		want                []goldenWorkerLine
	}{
		{"thu gốc 300 + lãi 50: 4 dòng, cặp gốc trước cặp lãi", 300_000_000, 50_000_000, []goldenWorkerLine{
			{"DEBIT", 300_000_000, "CASH_SETTLEMENT_ACCOUNT"},
			{"CREDIT", 300_000_000, "LNM_LOAN_PRINCIPAL"},
			{"DEBIT", 50_000_000, "CASH_SETTLEMENT_ACCOUNT"},
			{"CREDIT", 50_000_000, "LNM_INTEREST_RECEIVABLE"},
		}},
		{"chỉ thu gốc: 2 dòng card 1-2", 300_000_000, 0, []goldenWorkerLine{
			{"DEBIT", 300_000_000, "CASH_SETTLEMENT_ACCOUNT"},
			{"CREDIT", 300_000_000, "LNM_LOAN_PRINCIPAL"},
		}},
		{"chỉ thu lãi: 2 dòng card 3-4, đánh số lại từ 1", 0, 50_000_000, []goldenWorkerLine{
			{"DEBIT", 50_000_000, "CASH_SETTLEMENT_ACCOUNT"},
			{"CREDIT", 50_000_000, "LNM_INTEREST_RECEIVABLE"},
		}},
		{"không gốc không lãi: không có dòng", 0, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines, err := financeclient.PostingLinesFromRules("LNM_COLLECTION", goldenLoanWorkerCards["LNM_COLLECTION"], collectionLegs(base(tc.principal, tc.interest)), "VND")
			if err != nil {
				t.Fatalf("lỗi giải rule card: %v", err)
			}
			assertGoldenWorkerLines(t, lines, tc.want)
			for _, line := range lines {
				if line.GetDirection() == "CREDIT" && line.GetAnalytics().GetDebtGroupCode() != "GROUP_2" {
					t.Fatalf("dòng Có (giảm cho vay / phải thu lãi) phải mang nhóm nợ: %+v", line.GetAnalytics())
				}
			}
		})
	}
}

func TestGoldenBatchCollectionLegs(t *testing.T) {
	batch := &loanv1.BatchPostingDetail{BatchId: "batch-1", BatchType: "COLLECTION", CurrencyCode: "VND"}
	row := func(principal, interest, overdue int64) *loanv1.BatchRowDetail {
		return &loanv1.BatchRowDetail{ContractCode: "HD-001", AgreementCode: "KU-001", PlanCode: "P1",
			PrincipalMinor: principal, InterestMinor: interest, OverdueInterestMinor: overdue,
			DebtGroupCode: "GROUP_3", OrgUnitCode: "PGD01", CustomerCode: "KH-1"}
	}
	cases := []struct {
		name                         string
		principal, interest, overdue int64
		want                         []goldenWorkerLine
	}{
		{"gốc 300 + lãi 50 + lãi quá hạn 20: lãi quá hạn gộp vào cặp lãi = 70", 300, 50, 20, []goldenWorkerLine{
			{"DEBIT", 300, "CASH_SETTLEMENT_ACCOUNT"},
			{"CREDIT", 300, "LNM_LOAN_PRINCIPAL"},
			{"DEBIT", 70, "CASH_SETTLEMENT_ACCOUNT"},
			{"CREDIT", 70, "LNM_INTEREST_RECEIVABLE"},
		}},
		{"chỉ lãi quá hạn 20: vẫn đi cặp lãi card 3-4", 0, 0, 20, []goldenWorkerLine{
			{"DEBIT", 20, "CASH_SETTLEMENT_ACCOUNT"},
			{"CREDIT", 20, "LNM_INTEREST_RECEIVABLE"},
		}},
		{"dòng 0 đồng (đã đóng): không có dòng", 0, 0, 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legs := collectionBatchLegs(batch, row(tc.principal, tc.interest, tc.overdue))
			lines, err := financeclient.PostingLinesFromRules("LNM_COLLECTION", goldenLoanWorkerCards["LNM_COLLECTION"], legs, "VND")
			if err != nil {
				t.Fatalf("lỗi giải rule card: %v", err)
			}
			assertGoldenWorkerLines(t, lines, tc.want)
			for _, line := range lines {
				if line.GetAnalytics().GetDimensions()["batch_id"] != "batch-1" || line.GetDescription() != "HĐ HD-001 — KU-001 — P1" {
					t.Fatalf("dòng thiếu batch_id/mô tả theo kế hoạch: %+v", line)
				}
			}
		})
	}
}

// Batch nhiều hợp đồng: mỗi dòng một cặp, tổng Nợ = tổng Có = tổng hồ sơ,
// dòng đóng hợp đồng 0 đồng bị bỏ.
func TestGoldenBatchDisbursementLegsAcrossRows(t *testing.T) {
	batch := &loanv1.BatchPostingDetail{BatchId: "batch-2", BatchType: "DISB_REGISTER", CurrencyCode: "VND",
		Rows: []*loanv1.BatchRowDetail{
			{ContractCode: "HD-001", AgreementCode: "KU-001", AmountMinor: 100_000_000, DebtGroupCode: "GROUP_1", OrgUnitCode: "PGD01", CustomerCode: "KH-1"},
			{ContractCode: "HD-002", AgreementCode: "KU-002", AmountMinor: 250_000_000, DebtGroupCode: "GROUP_1", OrgUnitCode: "PGD01", CustomerCode: "KH-2"},
			{ContractCode: "HD-003", AgreementCode: "KU-003", AmountMinor: 0, IsClosed: true, OrgUnitCode: "PGD01"},
		}}
	var legs []financeclient.PostingLeg
	for _, r := range batch.GetRows() {
		legs = append(legs, disbursementBatchLegs(batch, r)...)
	}
	lines, err := financeclient.PostingLinesFromRules("LNM_DISB_REGISTER", goldenLoanWorkerCards["LNM_DISB_REGISTER"], legs, "VND")
	if err != nil {
		t.Fatalf("lỗi giải rule card: %v", err)
	}
	assertGoldenWorkerLines(t, lines, []goldenWorkerLine{
		{"DEBIT", 100_000_000, "LNM_LOAN_PRINCIPAL"},
		{"CREDIT", 100_000_000, "FUND_DISBURSEMENT_IN_TRANSIT"},
		{"DEBIT", 250_000_000, "LNM_LOAN_PRINCIPAL"},
		{"CREDIT", 250_000_000, "FUND_DISBURSEMENT_IN_TRANSIT"},
	})
	if lines[0].GetDescription() != "HĐ HD-001 — KU-001" || lines[2].GetAnalytics().GetDimensions()["agreement_code"] != "KU-002" {
		t.Fatalf("mỗi cặp phải mang hợp đồng/khế ước của dòng mình: %+v", lines)
	}
}

// Rule card thiếu dòng lãi (chỉ 2 dòng): thu lãi phải lỗi RULE_NOT_FOUND có
// kiểu, không rơi về classification hard-code (T3.3).
func TestGoldenCollectionInterestNeedsCardLines3And4(t *testing.T) {
	detail := &loanv1.CollectionPostingDetail{PrincipalMinor: 100, InterestMinor: 10}
	_, err := financeclient.PostingLinesFromRules("LNM_COLLECTION", goldenLoanWorkerCards["LNM_COLLECTION"][:2], collectionLegs(detail), "VND")
	typed, ok := financeclient.AsPostingError(err)
	if !ok || typed.Code != financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND {
		t.Fatalf("muốn RULE_NOT_FOUND, nhận %v", err)
	}
}
