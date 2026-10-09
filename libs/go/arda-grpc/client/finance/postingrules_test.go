package finance

import (
	"context"
	"errors"
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

type postingRulesStub struct {
	rules []*financev1.PostingRule
	err   error
}

func (s postingRulesStub) ListPostingRules(context.Context, string) ([]*financev1.PostingRule, error) {
	return s.rules, s.err
}

func TestBuildPostingLinesUsesCardClassifications(t *testing.T) {
	rules := postingRulesStub{rules: []*financev1.PostingRule{
		{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP", AccClassification: "LNM_LOAN_PRINCIPAL"},
		{LineNo: 2, Direction: "CREDIT", ResolutionType: "CLASS_MAP", AccClassification: "FUND_DISBURSEMENT_IN_TRANSIT"},
	}}
	legs := []PostingLeg{{CardLine: 1, Direction: "DEBIT", AmountMinor: 500, Analytics: &financev1.Analytics{ContractCode: "C1"}}, {CardLine: 2, Direction: "CREDIT", AmountMinor: 500}}
	lines, err := BuildPostingLines(context.Background(), rules, "LNM_DISBURSEMENT", legs, "VND")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines len = %d, want 2", len(lines))
	}
	if got := lines[0].GetAnalytics().GetAccClassification(); got != "LNM_LOAN_PRINCIPAL" {
		t.Fatalf("classification = %q", got)
	}
	if got := lines[0].GetAnalytics().GetContractCode(); got != "C1" {
		t.Fatalf("contract code lost: %q", got)
	}
	if lines[0].GetLineNo() != 1 || lines[1].GetLineNo() != 2 {
		t.Fatalf("line numbers = %d/%d", lines[0].GetLineNo(), lines[1].GetLineNo())
	}
}

func TestWithClassificationPreservesInputAndCopiesDimensions(t *testing.T) {
	input := &financev1.Analytics{
		AccClassification: "CALLER_VALUE",
		OrgUnitCode:       "OU-1",
		Dimensions:        map[string]string{"channel": "branch"},
	}
	got := WithClassification(input, "CARD_VALUE")
	if got.GetAccClassification() != "CARD_VALUE" || got.GetOrgUnitCode() != "OU-1" {
		t.Fatalf("resolved analytics = %+v", got)
	}
	if input.GetAccClassification() != "CALLER_VALUE" {
		t.Fatalf("input analytics mutated: %+v", input)
	}
	got.Dimensions["channel"] = "online"
	if input.GetDimensions()["channel"] != "branch" {
		t.Fatalf("input dimensions mutated: %+v", input.GetDimensions())
	}
}

func TestBuildPostingLinesMissingCardReturnsTypedRuleNotFound(t *testing.T) {
	_, err := BuildPostingLines(context.Background(), postingRulesStub{}, "LNM_COLLECTION", []PostingLeg{{CardLine: 1, Direction: "DEBIT", AmountMinor: 100}}, "VND")
	assertPostingCode(t, err, financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND)
}

func TestBuildPostingLinesMissingClassificationReturnsTypedAccountUnresolved(t *testing.T) {
	_, err := BuildPostingLines(context.Background(), postingRulesStub{rules: []*financev1.PostingRule{{LineNo: 1, Direction: "DEBIT", ResolutionType: "CLASS_MAP"}}}, "LNM_COLLECTION", []PostingLeg{{CardLine: 1, Direction: "DEBIT", AmountMinor: 100}}, "VND")
	assertPostingCode(t, err, financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED)
}

func TestBuildPostingLinesFixedCodeResolvesAccount(t *testing.T) {
	lines, err := BuildPostingLines(context.Background(), postingRulesStub{rules: []*financev1.PostingRule{{LineNo: 1, Direction: "DEBIT", ResolutionType: "FIXED_CODE", AccountRef: "1111"}}}, "CASH", []PostingLeg{{CardLine: 1, Direction: "DEBIT", AmountMinor: 10}}, "VND")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].GetAccountCode() != "1111" {
		t.Fatalf("fixed-code lines = %+v", lines)
	}
	if lines[0].GetAnalytics() != nil {
		t.Fatalf("fixed code should not set analytics: %+v", lines[0].GetAnalytics())
	}
}

func TestBuildPostingLinesRejectsMissingFixedAccountAndDirectionMismatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		rule *financev1.PostingRule
		want financev1.PostingErrorCode
	}{
		{"missing account", &financev1.PostingRule{LineNo: 1, Direction: "DEBIT", ResolutionType: "FIXED_CODE"}, financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED},
		{"direction mismatch", &financev1.PostingRule{LineNo: 1, Direction: "CREDIT", ResolutionType: "FIXED_CODE", AccountRef: "1111"}, financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildPostingLines(context.Background(), postingRulesStub{rules: []*financev1.PostingRule{tc.rule}}, "CASH", []PostingLeg{{CardLine: 1, Direction: "DEBIT", AmountMinor: 10}}, "VND")
			assertPostingCode(t, err, tc.want)
		})
	}
}

func TestBuildPostingLinesSkipsZeroLegsWithoutLookingUpCard(t *testing.T) {
	lines, err := BuildPostingLines(context.Background(), nil, "LNM_COLLECTION", []PostingLeg{{CardLine: 1, Direction: "DEBIT", AmountMinor: 0}}, "VND")
	if err != nil || len(lines) != 0 {
		t.Fatalf("zero lines = %v, err=%v", lines, err)
	}
}

func TestFetchPostingRulesPreservesTransportErrors(t *testing.T) {
	transportErr := errors.New("finance unavailable")
	_, err := FetchPostingRules(context.Background(), postingRulesStub{err: transportErr}, "LNM_ACCRUAL")
	if !errors.Is(err, transportErr) {
		t.Fatalf("fetch error = %v, want transport cause", err)
	}
	if _, ok := AsPostingError(err); ok {
		t.Fatalf("transport error must remain retryable, got business error %v", err)
	}
}

func assertPostingCode(t *testing.T, err error, want financev1.PostingErrorCode) {
	t.Helper()
	typed, ok := AsPostingError(err)
	if !ok {
		t.Fatalf("error %v is not typed", err)
	}
	if typed.Code != want {
		t.Fatalf("error code = %s, want %s", typed.Code, want)
	}
}
