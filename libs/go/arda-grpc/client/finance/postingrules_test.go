package finance

import (
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

func TestPostingLinesFromRulesUsesCardClassifications(t *testing.T) {
	rules := []*financev1.PostingRule{
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_LOAN_PRINCIPAL"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "FUND_DISBURSEMENT_IN_TRANSIT"},
	}
	legs := []PostingLeg{
		{CardLine: 1, Fallback: "WRONG_DEBIT", Direction: "DEBIT", AmountMinor: 500,
			Analytics: &financev1.Analytics{ContractCode: "C1"}},
		{CardLine: 2, Fallback: "WRONG_CREDIT", Direction: "CREDIT", AmountMinor: 500},
	}
	lines := PostingLinesFromRules(rules, legs, "VND")
	if len(lines) != 2 {
		t.Fatalf("lines len = %d, want 2", len(lines))
	}
	if got := lines[0].GetAnalytics().GetAccClassification(); got != "LNM_LOAN_PRINCIPAL" {
		t.Fatalf("line 1 classification = %q, want card classification", got)
	}
	// Caller-set analytics survive the classification stamp.
	if lines[0].GetAnalytics().GetContractCode() != "C1" {
		t.Fatalf("line 1 contract code lost: %+v", lines[0].GetAnalytics())
	}
	if got := lines[1].GetAnalytics().GetAccClassification(); got != "FUND_DISBURSEMENT_IN_TRANSIT" {
		t.Fatalf("line 2 classification = %q, want card classification", got)
	}
	if lines[0].GetLineNo() != 1 || lines[1].GetLineNo() != 2 {
		t.Fatalf("line numbers = %d/%d, want 1/2", lines[0].GetLineNo(), lines[1].GetLineNo())
	}
}

func TestPostingLinesFromRulesFallsBackWithoutCard(t *testing.T) {
	legs := []PostingLeg{
		{CardLine: 1, Fallback: "CASH_SETTLEMENT_ACCOUNT", Direction: "DEBIT", AmountMinor: 100},
		{CardLine: 2, Fallback: "LNM_LOAN_PRINCIPAL", Direction: "CREDIT", AmountMinor: 100},
	}
	lines := PostingLinesFromRules(nil, legs, "VND")
	if len(lines) != 2 {
		t.Fatalf("lines len = %d, want 2", len(lines))
	}
	if got := lines[0].GetAnalytics().GetAccClassification(); got != "CASH_SETTLEMENT_ACCOUNT" {
		t.Fatalf("line 1 classification = %q, want built-in fallback", got)
	}
	if got := lines[1].GetAnalytics().GetAccClassification(); got != "LNM_LOAN_PRINCIPAL" {
		t.Fatalf("line 2 classification = %q, want built-in fallback", got)
	}
}

func TestPostingLinesFromRulesSkipsZeroLegs(t *testing.T) {
	// Collection shape: the interest pair only posts when interest > 0; its
	// card lines (3/4) must still resolve even though legs 1/2 were skipped.
	rules := []*financev1.PostingRule{
		{LineNo: 3, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "CASH_SETTLEMENT_ACCOUNT"},
		{LineNo: 4, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_INTEREST_RECEIVABLE"},
	}
	legs := []PostingLeg{
		{CardLine: 1, Fallback: "CASH_SETTLEMENT_ACCOUNT", Direction: "DEBIT", AmountMinor: 0},
		{CardLine: 2, Fallback: "LNM_LOAN_PRINCIPAL", Direction: "CREDIT", AmountMinor: 0},
		{CardLine: 3, Fallback: "CASH", Direction: "DEBIT", AmountMinor: 250},
		{CardLine: 4, Fallback: "LNM_INTEREST_RECEIVABLE", Direction: "CREDIT", AmountMinor: 250},
	}
	lines := PostingLinesFromRules(rules, legs, "VND")
	if len(lines) != 2 {
		t.Fatalf("lines len = %d, want 2 (zero legs skipped)", len(lines))
	}
	if got := lines[0].GetAnalytics().GetAccClassification(); got != "CASH_SETTLEMENT_ACCOUNT" {
		t.Fatalf("interest debit classification = %q, want card row 3", got)
	}
	if got := lines[1].GetAnalytics().GetAccClassification(); got != "LNM_INTEREST_RECEIVABLE" {
		t.Fatalf("interest credit classification = %q, want card row 4", got)
	}
	if lines[0].GetLineNo() != 1 || lines[1].GetLineNo() != 2 {
		t.Fatalf("output line numbers = %d/%d, want renumbered 1/2", lines[0].GetLineNo(), lines[1].GetLineNo())
	}
}

func TestPostingLinesFromRulesFixedCodeResolvesAccountDirectly(t *testing.T) {
	rules := []*financev1.PostingRule{
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "FIXED_CODE", AccountRef: "1111"},
	}
	legs := []PostingLeg{{CardLine: 1, Fallback: "CASH", Direction: "DEBIT", AmountMinor: 10}}
	lines := PostingLinesFromRules(rules, legs, "VND")
	if len(lines) != 1 || lines[0].GetAccountCode() != "1111" {
		t.Fatalf("fixed-code line = %+v, want account_code 1111", lines)
	}
	if lines[0].GetAnalytics() != nil {
		t.Fatalf("fixed-code line should resolve directly, got analytics %+v", lines[0].GetAnalytics())
	}
}

func TestFetchPostingRulesNilClient(t *testing.T) {
	// A nil client degrades to nil — the caller falls back to its built-in
	// legs without failing the flow. A dial failure or an unseeded document
	// type degrades through the error/empty paths of Client.ListPostingRules.
	if got := FetchPostingRules(nil, "LNM_ACCRUAL"); got != nil {
		t.Fatalf("nil client = %+v, want nil", got)
	}
}
