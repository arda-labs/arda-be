package service

// Bảng case chuẩn (cần DB) — PostingService theo accounting-posting-contract.md:
//
//   1. ValidatePosting: cân Σ Nợ = Σ Có THEO TỪNG TIỀN TỆ (UNBALANCED:<ccy>),
//      số tiền phải dương (INVALID_AMOUNT), phải có classification
//      (CLASSIFICATION_REQUIRED), classification không map được
//      (ACCOUNT_UNRESOLVED), map theo nhóm nợ ưu tiên hơn map chung, không
//      dòng (NO_LINES), kỳ đóng (PERIOD_CLOSED).
//   2. Rule card seed (T3.3): từng document type → đúng (line, chiều,
//      classification) như docs/accounting-rule-cards.md.
//   3. Trial balance: post hai bút toán + số dư đầu kỳ + một bút toán đảo →
//      Nợ/Có/số dư từng tài khoản, tổng Nợ = tổng Có.
//
// Chạy khi ARDA_TEST_DSN trỏ Postgres 18 (testdb). Số tiền minor unit.

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/migration"
	"github.com/arda-labs/arda/apps/finance-service/internal/repository"
	"github.com/arda-labs/arda/libs/go/arda-postgres/testdb"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

const goldenFinTenant = "00000000-0000-0000-0000-000000000010"

func openGoldenFinance(t *testing.T) (*sql.DB, *PostingService) {
	t.Helper()
	db := testdb.Open(t, func(db *sql.DB) error { return migration.Run(db, "postgres") })
	t.Cleanup(func() { _ = db.Close() })
	tn := goldenFinTenant
	for _, stmt := range []string{
		`INSERT INTO fin_coa_versions (tenant_id, code, name, effective_date) VALUES ('` + tn + `','V1','COA 2026','2026-01-01') ON CONFLICT (tenant_id, code) DO NOTHING`,
		`INSERT INTO fin_coa_accounts (tenant_id, version_code, acc_code, name, acc_type, acc_nature) VALUES
		   ('` + tn + `','V1','1131','Tiền gửi ngân hàng','ASSET','D'),
		   ('` + tn + `','V1','1311','Cho vay khách hàng','ASSET','D'),
		   ('` + tn + `','V1','1312','Cho vay khách hàng nhóm 2','ASSET','D'),
		   ('` + tn + `','V1','5111','Doanh thu lãi cho vay','INCOME','C'),
		   ('` + tn + `','V1','1132','Tiền gửi ngoại tệ','ASSET','D')
		 ON CONFLICT (tenant_id, version_code, acc_code) DO NOTHING`,
		`INSERT INTO fin_acc_class_coa_maps (tenant_id, classification, coa_version, coa_acc_code, effective_date, debt_group_code, currency_code) VALUES
		   ('` + tn + `','CASH_SETTLEMENT_ACCOUNT','V1','1131','2026-01-01','',''),
		   ('` + tn + `','CASH_SETTLEMENT_ACCOUNT','V1','1132','2026-01-01','','USD'),
		   ('` + tn + `','LNM_LOAN_PRINCIPAL','V1','1311','2026-01-01','',''),
		   ('` + tn + `','LNM_LOAN_PRINCIPAL','V1','1312','2026-01-01','GROUP_2',''),
		   ('` + tn + `','LNM_INTEREST_INCOME','V1','5111','2026-01-01','','')
		 ON CONFLICT (tenant_id, classification, coa_version, coa_acc_code, debt_group_code, currency_code) DO NOTHING`,
		`INSERT INTO fin_periods (tenant_id, period_code, start_date, end_date, status) VALUES
		   ('` + tn + `','2026-08','2026-08-01','2026-08-31','CLOSED'),
		   ('` + tn + `','2026-09','2026-09-01','2026-09-30','OPEN')
		 ON CONFLICT (tenant_id, period_code) DO NOTHING`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	return db, NewPostingService(repository.NewPostingRepository(db), db)
}

func goldenLine(no int32, dir string, amount int64, class, debtGroup, currency string) *financev1.PostingLine {
	return &financev1.PostingLine{LineNo: no, Direction: dir, AmountMinor: amount, CurrencyCode: currency,
		Analytics: &financev1.Analytics{AccClassification: class, DebtGroupCode: debtGroup, OrgUnitCode: "HO", ContractCode: "HD-001"}}
}

func goldenRequest(key, date string, lines ...*financev1.PostingLine) *financev1.PostingRequest {
	return &financev1.PostingRequest{IdempotencyKey: key, AccountingDate: date, CurrencyCode: "VND", Description: key,
		BusinessReference: &financev1.BusinessReference{Domain: "lnm", DocumentType: "LNM_COLLECTION", DocumentCode: key}, Lines: lines}
}

func TestGoldenValidatePostingRules(t *testing.T) {
	_, svc := openGoldenFinance(t)
	ctx := context.Background()
	cases := []struct {
		name         string
		req          *financev1.PostingRequest
		wantValid    bool
		wantGlobal   []string
		wantLineErrs map[int32][]string
		wantAccounts map[int32]string
	}{
		{"cân VND 300 Nợ / 300 Có → hợp lệ, resolve 1131/1311", goldenRequest("v1", "2026-09-07",
			goldenLine(1, "DEBIT", 300, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 300, "LNM_LOAN_PRINCIPAL", "", "")),
			true, nil, nil, map[int32]string{1: "1131", 2: "1311"}},
		{"lệch 1 đồng → UNBALANCED:VND", goldenRequest("v2", "2026-09-07",
			goldenLine(1, "DEBIT", 300, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 299, "LNM_LOAN_PRINCIPAL", "", "")),
			false, []string{"UNBALANCED:VND"}, nil, nil},
		{"nhiều dòng cùng chiều: 300 = 200 + 100 cân", goldenRequest("v3", "2026-09-07",
			goldenLine(1, "DEBIT", 300, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 200, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(3, "CREDIT", 100, "LNM_INTEREST_INCOME", "", "")),
			true, nil, nil, map[int32]string{3: "5111"}},
		{"hai tiền tệ, mỗi tiền tệ tự cân → hợp lệ", goldenRequest("v4", "2026-09-07",
			goldenLine(1, "DEBIT", 300, "CASH_SETTLEMENT_ACCOUNT", "", "VND"), goldenLine(2, "CREDIT", 300, "LNM_LOAN_PRINCIPAL", "", "VND"),
			goldenLine(3, "DEBIT", 50, "CASH_SETTLEMENT_ACCOUNT", "", "USD"), goldenLine(4, "CREDIT", 50, "LNM_LOAN_PRINCIPAL", "", "USD")),
			true, nil, nil, map[int32]string{3: "1132"}},
		{"VND cân nhưng USD lệch → chỉ UNBALANCED:USD (không bù chéo tiền tệ)", goldenRequest("v5", "2026-09-07",
			goldenLine(1, "DEBIT", 300, "CASH_SETTLEMENT_ACCOUNT", "", "VND"), goldenLine(2, "CREDIT", 300, "LNM_LOAN_PRINCIPAL", "", "VND"),
			goldenLine(3, "DEBIT", 50, "CASH_SETTLEMENT_ACCOUNT", "", "USD"), goldenLine(4, "CREDIT", 40, "LNM_LOAN_PRINCIPAL", "", "USD")),
			false, []string{"UNBALANCED:USD"}, nil, nil},
		{"số tiền 0 → INVALID_AMOUNT ở dòng đó", goldenRequest("v6", "2026-09-07",
			goldenLine(1, "DEBIT", 0, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 0, "LNM_LOAN_PRINCIPAL", "", "")),
			false, nil, map[int32][]string{1: {"INVALID_AMOUNT"}, 2: {"INVALID_AMOUNT"}}, nil},
		{"số tiền âm → INVALID_AMOUNT (và lệch vì −100 ≠ 100)", goldenRequest("v7", "2026-09-07",
			goldenLine(1, "DEBIT", -100, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 100, "LNM_LOAN_PRINCIPAL", "", "")),
			false, []string{"UNBALANCED:VND"}, map[int32][]string{1: {"INVALID_AMOUNT"}}, nil},
		{"thiếu classification → CLASSIFICATION_REQUIRED", goldenRequest("v8", "2026-09-07",
			goldenLine(1, "DEBIT", 100, "", "", ""), goldenLine(2, "CREDIT", 100, "LNM_LOAN_PRINCIPAL", "", "")),
			false, nil, map[int32][]string{1: {"CLASSIFICATION_REQUIRED"}}, nil},
		{"classification chưa map COA → ACCOUNT_UNRESOLVED", goldenRequest("v9", "2026-09-07",
			goldenLine(1, "DEBIT", 100, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 100, "GOLDEN_UNMAPPED_CLASS", "", "")),
			false, nil, map[int32][]string{2: {"ACCOUNT_UNRESOLVED"}}, nil},
		{"map theo nhóm nợ: GROUP_2 → 1312, nhóm khác → map chung 1311", goldenRequest("v10", "2026-09-07",
			goldenLine(1, "DEBIT", 100, "LNM_LOAN_PRINCIPAL", "GROUP_2", ""), goldenLine(2, "CREDIT", 100, "LNM_LOAN_PRINCIPAL", "GROUP_3", "")),
			true, nil, nil, map[int32]string{1: "1312", 2: "1311"}},
		{"chiều lạ → INVALID_DIRECTION, và không được tính vào tổng nên còn UNBALANCED", goldenRequest("v11", "2026-09-07",
			goldenLine(1, "DR", 100, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 100, "LNM_LOAN_PRINCIPAL", "", "")),
			false, []string{"UNBALANCED:VND"}, map[int32][]string{1: {"INVALID_DIRECTION"}}, nil},
		{"không có dòng → NO_LINES", goldenRequest("v12", "2026-09-07"),
			false, []string{"NO_LINES"}, nil, nil},
		{"kỳ 2026-08 đã đóng → PERIOD_CLOSED dù bút toán cân", goldenRequest("v13", "2026-08-15",
			goldenLine(1, "DEBIT", 100, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 100, "LNM_LOAN_PRINCIPAL", "", "")),
			false, []string{"PERIOD_CLOSED"}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := svc.ValidatePosting(ctx, goldenFinTenant, tc.req)
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if res.GetValid() != tc.wantValid {
				t.Fatalf("valid = %v, muốn %v (global %v, lines %v)", res.GetValid(), tc.wantValid, res.GetGlobalErrors(), res.GetLines())
			}
			got := append([]string(nil), res.GetGlobalErrors()...)
			want := append([]string(nil), tc.wantGlobal...)
			sort.Strings(got)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("global errors = %v, muốn %v", got, want)
			}
			for _, line := range res.GetLines() {
				wantErrs := tc.wantLineErrs[line.GetLineNo()]
				if strings.Join(line.GetErrors(), ",") != strings.Join(wantErrs, ",") {
					t.Fatalf("dòng %d errors = %v, muốn %v", line.GetLineNo(), line.GetErrors(), wantErrs)
				}
				if acc, ok := tc.wantAccounts[line.GetLineNo()]; ok && line.GetAccountCode() != acc {
					t.Fatalf("dòng %d resolve %q, muốn %q", line.GetLineNo(), line.GetAccountCode(), acc)
				}
			}
		})
	}
}

func TestGoldenSeededRuleCards(t *testing.T) {
	_, svc := openGoldenFinance(t)
	ctx := context.Background()
	type card struct {
		line      int32
		direction string
		class     string
	}
	cases := map[string][]card{
		"LNM_DISB_REGISTER": {{1, "DEBIT", "LNM_LOAN_PRINCIPAL"}, {2, "CREDIT", "FUND_DISBURSEMENT_IN_TRANSIT"}},
		"LNM_DISB_COMPLETE": {{1, "DEBIT", "FUND_DISBURSEMENT_IN_TRANSIT"}, {2, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"LNM_COLLECTION":    {{1, "DEBIT", "CASH_SETTLEMENT_ACCOUNT"}, {2, "CREDIT", "LNM_LOAN_PRINCIPAL"}, {3, "DEBIT", "CASH_SETTLEMENT_ACCOUNT"}, {4, "CREDIT", "LNM_INTEREST_RECEIVABLE"}},
		"LNM_ACCRUAL":       {{1, "DEBIT", "LNM_INTEREST_RECEIVABLE"}, {2, "CREDIT", "LNM_INTEREST_INCOME"}},
		"LNM_PROVISION":     {{1, "DEBIT", "LNM_PROVISION_EXPENSE"}, {2, "CREDIT", "LNM_PROVISION_LIABILITY"}, {3, "DEBIT", "LNM_PROVISION_LIABILITY"}, {4, "CREDIT", "LNM_PROVISION_RELEASE"}},
		"LNM_PROVISION_306": {{1, "DEBIT", "LNM_PROVISION_EXPENSE"}, {2, "CREDIT", "LNM_PROVISION_LIABILITY"}, {3, "DEBIT", "LNM_PROVISION_LIABILITY"}, {4, "CREDIT", "LNM_PROVISION_RELEASE"}},
		"DPM_OPEN":          {{1, "DEBIT", "CASH_SETTLEMENT_ACCOUNT"}, {2, "CREDIT", "DPM_DEPOSIT_LIABILITY"}},
		"DPM_SETTLEMENT":    {{1, "DEBIT", "DPM_DEPOSIT_LIABILITY"}, {2, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"DPM_SETTLEMENT_V3": {{1, "DEBIT", "DPM_DEPOSIT_LIABILITY"}, {2, "DEBIT", "DPM_INTEREST_PAYABLE"}, {3, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"DPM_ACCRUAL":       {{1, "DEBIT", "DPM_INTEREST_EXPENSE"}, {2, "CREDIT", "DPM_INTEREST_PAYABLE"}},
		"DPM_INTEREST_PAY":  {{1, "DEBIT", "DPM_INTEREST_PAYABLE"}, {2, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"DPM_CAPITALIZE":    {{1, "DEBIT", "DPM_INTEREST_PAYABLE"}, {2, "CREDIT", "DPM_DEPOSIT_LIABILITY"}},
		"CFC_RECEIPT":       {{1, "DEBIT", "CASH_SETTLEMENT_ACCOUNT"}, {2, "CREDIT", "FUND_CAPITAL_LIABILITY"}},
		"CFC_DISBURSEMENT":  {{1, "DEBIT", "FUND_CAPITAL_LIABILITY"}, {2, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"CFC_PAYMENT":       {{1, "DEBIT", "CFC_INTEREST_EXPENSE"}, {2, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"CFC_SETTLEMENT":    {{1, "DEBIT", "FUND_CAPITAL_LIABILITY"}, {2, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"IBM_PLACE":         {{1, "DEBIT", "IBM_INTERBANK_DEPOSIT"}, {2, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"IBM_TOP_UP":        {{1, "DEBIT", "IBM_INTERBANK_DEPOSIT"}, {2, "CREDIT", "CASH_SETTLEMENT_ACCOUNT"}},
		"IBM_INTEREST":      {{1, "DEBIT", "CASH_SETTLEMENT_ACCOUNT"}, {2, "CREDIT", "IBM_INTEREST_INCOME"}},
		"IBM_EXPECTED":      {{1, "DEBIT", "IBM_INTEREST_RECEIVABLE"}, {2, "CREDIT", "IBM_INTEREST_INCOME"}},
		"IBM_WITHDRAW":      {{1, "DEBIT", "CASH_SETTLEMENT_ACCOUNT"}, {2, "CREDIT", "IBM_INTERBANK_DEPOSIT"}},
	}
	for docType, want := range cases {
		t.Run(docType, func(t *testing.T) {
			rules, err := svc.ListPostingRules(ctx, goldenFinTenant, docType)
			if err != nil {
				t.Fatalf("list rules: %v", err)
			}
			if len(rules) != len(want) {
				t.Fatalf("card %s có %d dòng, muốn %d: %+v", docType, len(rules), len(want), rules)
			}
			for i, w := range want {
				r := rules[i]
				if r.GetLineNo() != w.line || r.GetDirection() != w.direction || r.GetResolutionType() != "CLASS_MAP" || r.GetAccClassification() != w.class {
					t.Fatalf("card %s dòng %d = %d %s %s %s, muốn %d %s CLASS_MAP %s", docType, i+1,
						r.GetLineNo(), r.GetDirection(), r.GetResolutionType(), r.GetAccClassification(), w.line, w.direction, w.class)
				}
			}
		})
	}
}

func TestGoldenTrialBalance(t *testing.T) {
	db, svc := openGoldenFinance(t)
	ctx := context.Background()
	if _, err := svc.UpsertOpeningBalance(ctx, goldenFinTenant, OpeningBalanceInput{
		AccountingDate: "2026-09-01", CoaVersion: "V1", AccountCode: "1131", CurrencyCode: "VND",
		Direction: "DEBIT", AmountMinor: 1_000_000, Description: "đầu kỳ", SourceKey: "ob-1131", Actor: "golden"}); err != nil {
		t.Fatalf("opening: %v", err)
	}
	// Giải ngân 600.000: Nợ 1311 / Có 1131. Thu nợ 300.000 gốc + 10.000 lãi.
	disb, err := svc.PostTransaction(ctx, goldenFinTenant, goldenRequest("tb-disb", "2026-09-05",
		goldenLine(1, "DEBIT", 600_000, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(2, "CREDIT", 600_000, "CASH_SETTLEMENT_ACCOUNT", "", "")))
	if err != nil {
		t.Fatalf("post disb: %v", err)
	}
	if _, err := svc.PostTransaction(ctx, goldenFinTenant, goldenRequest("tb-coll", "2026-09-07",
		goldenLine(1, "DEBIT", 310_000, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 300_000, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(3, "CREDIT", 10_000, "LNM_INTEREST_INCOME", "", ""))); err != nil {
		t.Fatalf("post coll: %v", err)
	}
	// Replay cùng idempotency key không nhân đôi số dư.
	if _, err := svc.PostTransaction(ctx, goldenFinTenant, goldenRequest("tb-coll", "2026-09-07",
		goldenLine(1, "DEBIT", 310_000, "CASH_SETTLEMENT_ACCOUNT", "", ""), goldenLine(2, "CREDIT", 300_000, "LNM_LOAN_PRINCIPAL", "", ""), goldenLine(3, "CREDIT", 10_000, "LNM_INTEREST_INCOME", "", ""))); err != nil {
		t.Fatalf("replay coll: %v", err)
	}
	// Bút toán đã bị đảo chưa đủ sức: kỳ 2026-09 vẫn mở nên đảo hợp lệ.
	type want struct{ debit, credit, balance int64 }
	assertTB := func(t *testing.T, asOf string, wants map[string]want) {
		t.Helper()
		tb, err := NewTrialBalanceService(db).TrialBalance(ctx, goldenFinTenant, asOf)
		if err != nil {
			t.Fatalf("trial balance %s: %v", asOf, err)
		}
		got := map[string]want{}
		for _, e := range tb.Entries {
			got[e.AccountCode] = want{e.DebitMinor, e.CreditMinor, e.BalanceMinor}
		}
		for acc, w := range wants {
			if got[acc] != w {
				t.Fatalf("%s TK %s = %+v, muốn %+v (toàn bộ %v)", asOf, acc, got[acc], w, got)
			}
		}
		if len(got) != len(wants) {
			t.Fatalf("%s có %d tài khoản, muốn %d: %v", asOf, len(got), len(wants), got)
		}
		if tb.TotalDebitMinor != tb.TotalCreditMinor {
			t.Fatalf("%s tổng Nợ %d ≠ tổng Có %d", asOf, tb.TotalDebitMinor, tb.TotalCreditMinor)
		}
	}
	t.Run("đến 06/09: chỉ đầu kỳ + giải ngân", func(t *testing.T) {
		assertTB(t, "2026-09-06", map[string]want{
			"1131": {0, 600_000, 400_000},
			"1311": {600_000, 0, 600_000},
		})
	})
	t.Run("đến 07/09: thêm thu nợ, replay không nhân đôi", func(t *testing.T) {
		assertTB(t, "2026-09-07", map[string]want{
			"1131": {310_000, 600_000, 710_000},
			"1311": {600_000, 300_000, 300_000},
			"5111": {0, 10_000, -10_000},
		})
	})
	t.Run("đảo bút toán giải ngân ngày 08/09: dòng đảo cộng vào cả hai bên, số dư 1311 về 0 từ giải ngân", func(t *testing.T) {
		if _, err := svc.ReverseTransaction(ctx, &financev1.ReverseRequest{TenantId: goldenFinTenant, JournalEntryId: disb.GetJournalEntryId(),
			Reason: "golden", IdempotencyKey: "tb-disb-rev", Actor: "golden", AccountingDate: "2026-09-08"}); err != nil {
			t.Fatalf("reverse: %v", err)
		}
		assertTB(t, "2026-09-08", map[string]want{
			"1131": {910_000, 600_000, 1_310_000},
			"1311": {600_000, 900_000, -300_000},
			"5111": {0, 10_000, -10_000},
		})
	})
	t.Run("trước ngày đầu kỳ: không có tài khoản nào", func(t *testing.T) {
		assertTB(t, "2026-08-31", map[string]want{})
	})
	_ = fmt.Sprint
}
