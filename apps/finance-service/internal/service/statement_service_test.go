package service

import (
	"testing"
)

// evaluateStatement is pure — tests exercise the real evaluator with the
// seed CDKT shape: debit-nature asset rows, a credit-nature provision row
// (sign -1), a chained asset total, and a grouping header without amount.
func TestEvaluateStatementCDKT(t *testing.T) {
	defs := []statementDef{
		{RowCode: "ASSETS", Label: "TÀI SẢN", Level: 0, SortOrder: 10, Sign: 1, Formula: formula{Type: "none"}},
		{RowCode: "CASH", ParentCode: "ASSETS", Label: "Tiền mặt tại quỹ", Level: 1, SortOrder: 20, Sign: 1, Formula: formula{Type: "accounts", Codes: []string{"1011"}}},
		{RowCode: "BANK", ParentCode: "ASSETS", Label: "Tiền gửi ngân hàng", Level: 1, SortOrder: 30, Sign: 1, Formula: formula{Type: "accounts", Codes: []string{"1131"}}},
		{RowCode: "LOANS_GROSS", ParentCode: "ASSETS", Label: "Cho vay khách hàng", Level: 1, SortOrder: 40, Sign: 1, Formula: formula{Type: "accounts", Codes: []string{"1311"}}},
		{RowCode: "PROVISION", ParentCode: "ASSETS", Label: "Dự phòng", Level: 1, SortOrder: 60, Sign: -1, Formula: formula{Type: "accounts", Codes: []string{"1319"}}},
		{RowCode: "ASSETS_TOTAL", ParentCode: "ASSETS", Label: "Tổng tài sản", Level: 1, SortOrder: 70, Sign: 1, IsTotal: true, Formula: formula{Type: "rows", Members: []formulaMember{{Code: "CASH"}, {Code: "BANK"}, {Code: "LOANS_GROSS"}, {Code: "PROVISION", Sign: -1}}}},
	}
	// Net debit-positive balances: 1319 carries a credit balance → negative.
	acctBal := map[string]int64{
		"1011": 500,
		"1131": 12_000_000,
		"1311": 50_000_000,
		"1319": -300_000,
	}
	res, err := evaluateStatement(defs, statementInputs{asOf: acctBal})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	byCode := map[string]StatementRow{}
	for _, r := range res.Rows {
		byCode[r.RowCode] = r
	}
	// Totals aggregate pre-sign ledger nets: the sign=-1 provision enters
	// its total once, with its ledger (negative) sign.
	if got := byCode["ASSETS_TOTAL"].AmountMinor; got != 50_000_000+12_000_000+500-300_000 {
		t.Fatalf("ASSETS_TOTAL = %d, want %d", got, 50_000_000+12_000_000+500-300_000)
	}
	// Presentation: the sign=-1 row displays its credit balance positive.
	if got := byCode["PROVISION"].AmountMinor; got != 300_000 {
		t.Fatalf("PROVISION (sign -1 on credit balance) = %d, want 300000", got)
	}
	if byCode["ASSETS"].HasAmount {
		t.Fatal("grouping row must not carry an amount")
	}
	if !byCode["ASSETS_TOTAL"].IsTotal {
		t.Fatal("total row must be flagged")
	}
}

// B02 seed shape: revenue accounts are credit-nature (negative nets,
// sign -1), expense contra-member inside profit, profit flips the P&L net.
func TestEvaluateStatementB02Profit(t *testing.T) {
	defs := []statementDef{
		{RowCode: "INT_INCOME", SortOrder: 20, Sign: -1, Formula: formula{Type: "accounts", Codes: []string{"5111"}}},
		{RowCode: "INCOME_TOTAL", SortOrder: 40, Sign: 1, IsTotal: true, Formula: formula{Type: "rows", Members: []formulaMember{{Code: "INT_INCOME"}}}},
		{RowCode: "PROVISION_EXP", SortOrder: 60, Sign: -1, Formula: formula{Type: "accounts", Codes: []string{"1319"}}},
		{RowCode: "EXPENSE_TOTAL", SortOrder: 70, Sign: 1, IsTotal: true, Formula: formula{Type: "rows", Members: []formulaMember{{Code: "PROVISION_EXP"}}}},
		{RowCode: "PROFIT", SortOrder: 80, Sign: 1, IsTotal: true, Formula: formula{Type: "rows", Members: []formulaMember{{Code: "INCOME_TOTAL"}, {Code: "EXPENSE_TOTAL", Sign: -1}}}},
	}
	acctBal := map[string]int64{"5111": -3_000_000, "1319": -1_000_000}
	res, err := evaluateStatement(defs, statementInputs{asOf: acctBal})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	byCode := map[string]StatementRow{}
	for _, r := range res.Rows {
		byCode[r.RowCode] = r
	}
	if got := byCode["INCOME_TOTAL"].AmountMinor; got != 3_000_000 {
		t.Fatalf("INCOME_TOTAL = %d, want 3000000", got)
	}
	if got := byCode["EXPENSE_TOTAL"].AmountMinor; got != 1_000_000 {
		t.Fatalf("EXPENSE_TOTAL = %d, want 1000000", got)
	}
	// P&L: expense enters PROFIT with role sign -1 (income display 3M −
	// expense display 1M).
	if got := byCode["PROFIT"].AmountMinor; got != 2_000_000 {
		t.Fatalf("PROFIT = %d, want 2000000", got)
	}
}

func TestEvaluateStatementUnknownFormulaType(t *testing.T) {
	defs := []statementDef{
		{RowCode: "R1", Label: "bad", SortOrder: 1, Sign: 1, Formula: formula{Type: "sql", Codes: []string{"select 1"}}},
	}
	if _, err := evaluateStatement(defs, statementInputs{}); err == nil {
		t.Fatal("unknown formula type must be rejected")
	}
}

func TestEvaluateStatementRowRefsMustPrecedeTotal(t *testing.T) {
	// A rows-formula referencing an unevaluated member silently sums 0 —
	// assert the documented behavior so seed mistakes are visible in review.
	defs := []statementDef{
		{RowCode: "TOTAL", Label: "total", SortOrder: 1, Sign: 1, Formula: formula{Type: "rows", Codes: []string{"MISSING"}}},
	}
	res, err := evaluateStatement(defs, statementInputs{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Rows[0].AmountMinor != 0 {
		t.Fatalf("unresolved member must contribute 0, got %d", res.Rows[0].AmountMinor)
	}
}

// B03 cash-flow shape: opening balance at period start + net movement =
// closing balance, all over the cash/settlement accounts.
func TestEvaluateStatementB03CashFlow(t *testing.T) {
	defs := []statementDef{
		{RowCode: "CASHFLOW", Label: "LƯU CHUYỂN TIỀN TỆ", Level: 0, SortOrder: 10, Sign: 1, Formula: formula{Type: "none"}},
		{RowCode: "OPENING_CASH", ParentCode: "CASHFLOW", Label: "Tiền đầu kỳ", Level: 1, SortOrder: 20, Sign: 1, Formula: formula{Type: "opening", Codes: []string{"1011", "1131"}}},
		{RowCode: "NET_CASH", ParentCode: "CASHFLOW", Label: "Lưu chuyển tiền thuần trong kỳ", Level: 1, SortOrder: 30, Sign: 1, Formula: formula{Type: "movement", Codes: []string{"1011", "1131"}}},
		{RowCode: "CLOSING_CASH", ParentCode: "CASHFLOW", Label: "Tiền cuối kỳ", Level: 1, SortOrder: 40, Sign: 1, IsTotal: true, Formula: formula{Type: "rows", Members: []formulaMember{{Code: "OPENING_CASH"}, {Code: "NET_CASH"}}}},
	}
	res, err := evaluateStatement(defs, statementInputs{
		opening:  map[string]int64{"1011": 100, "1131": 900},
		movement: map[string]int64{"1011": 50, "1131": -200},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	byCode := map[string]StatementRow{}
	for _, r := range res.Rows {
		byCode[r.RowCode] = r
	}
	if got := byCode["OPENING_CASH"].AmountMinor; got != 1_000 {
		t.Fatalf("OPENING_CASH = %d, want 1000", got)
	}
	if got := byCode["NET_CASH"].AmountMinor; got != -150 {
		t.Fatalf("NET_CASH = %d, want -150", got)
	}
	if got := byCode["CLOSING_CASH"].AmountMinor; got != 850 {
		t.Fatalf("CLOSING_CASH = %d, want 850", got)
	}
}
