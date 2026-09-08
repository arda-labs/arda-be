package service

import (
	"strings"
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

func line(no int32, direction string, amountMinor int64, accountCode string, currencyCode string) *financev1.PostingLine {
	return &financev1.PostingLine{
		LineNo:       no,
		Direction:    direction,
		AmountMinor:  amountMinor,
		AccountCode:  accountCode,
		CurrencyCode: currencyCode,
	}
}

func TestValidateManualPostingFlowRejectsUnknownFlow(t *testing.T) {
	req := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 1000, "1111", "VND"),
			line(2, "CREDIT", 1000, "5111", "VND"),
		},
	}
	err := validateManualPostingFlow("TRIPLE_ENTRY", req)
	if err == nil || !strings.Contains(err.Error(), "SINGLE_ENTRY, DOUBLE_ENTRY or OFF_BALANCE") {
		t.Fatalf("unknown flow error = %v, want flow rejection", err)
	}
}

func TestValidateManualPostingFlowRequiresPostingRequest(t *testing.T) {
	if err := validateManualPostingFlow(FlowSingleEntry, nil); err == nil {
		t.Fatal("nil posting request must be rejected")
	}
	if err := validateManualPostingFlow(FlowSingleEntry, &financev1.PostingRequest{AccountingDate: "2026-09-08"}); err == nil {
		t.Fatal("empty lines must be rejected")
	}
	if err := validateManualPostingFlow(FlowSingleEntry, &financev1.PostingRequest{Lines: []*financev1.PostingLine{line(1, "DEBIT", 1, "1111", "VND")}}); err == nil || !strings.Contains(err.Error(), "accounting_date") {
		t.Fatalf("missing accounting date error = %v", err)
	}
	if err := validateManualPostingFlow(FlowSingleEntry, &financev1.PostingRequest{AccountingDate: "08-09-2026", Lines: []*financev1.PostingLine{line(1, "DEBIT", 1, "1111", "VND"), line(2, "CREDIT", 1, "5111", "VND")}}); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("bad accounting date error = %v", err)
	}
	if err := validateManualPostingFlow(FlowSingleEntry, &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines:          []*financev1.PostingLine{line(1, "DEBIT", 1, "", "VND"), line(2, "CREDIT", 1, "5111", "VND")},
	}); err == nil || !strings.Contains(err.Error(), "account_code is required") {
		t.Fatalf("missing account_code error = %v", err)
	}
}

func TestValidateManualPostingFlowSingleEntry(t *testing.T) {
	valid := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 250_000, "1111", "VND"),
			line(2, "CREDIT", 250_000, "5111", "VND"),
		},
	}
	if err := validateManualPostingFlow(FlowSingleEntry, valid); err != nil {
		t.Fatalf("valid single entry rejected: %v", err)
	}

	// Lines may arrive in either direction order.
	flipped := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "CREDIT", 250_000, "5111", "VND"),
			line(2, "DEBIT", 250_000, "1111", "VND"),
		},
	}
	if err := validateManualPostingFlow(FlowSingleEntry, flipped); err != nil {
		t.Fatalf("flipped single entry rejected: %v", err)
	}

	if err := validateManualPostingFlow(FlowSingleEntry, &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines:          []*financev1.PostingLine{line(1, "DEBIT", 250_000, "1111", "VND")},
	}); err == nil || !strings.Contains(err.Error(), "exactly 2 lines") {
		t.Fatalf("one-line single entry error = %v", err)
	}

	threeLines := &financev1.PostingRequest{AccountingDate: "2026-09-08", Lines: append(append([]*financev1.PostingLine{}, valid.Lines...), line(3, "CREDIT", 0, "5111", "VND"))}
	if err := validateManualPostingFlow(FlowSingleEntry, threeLines); err == nil || !strings.Contains(err.Error(), "exactly 2 lines") {
		t.Fatalf("three-line single entry error = %v", err)
	}

	bothDebit := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 250_000, "1111", "VND"),
			line(2, "DEBIT", 250_000, "5111", "VND"),
		},
	}
	if err := validateManualPostingFlow(FlowSingleEntry, bothDebit); err == nil || !strings.Contains(err.Error(), "one CREDIT") {
		t.Fatalf("debit+debit single entry error = %v", err)
	}

	unequal := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 250_000, "1111", "VND"),
			line(2, "CREDIT", 150_000, "5111", "VND"),
		},
	}
	if err := validateManualPostingFlow(FlowSingleEntry, unequal); err == nil || !strings.Contains(err.Error(), "equal debit and credit amounts") {
		t.Fatalf("unequal single entry error = %v", err)
	}
}

func TestValidateManualPostingFlowOffBalance(t *testing.T) {
	validDebit := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 100_000, "091", "VND"),
			line(2, "DEBIT", 100_000, "092", "VND"),
			line(3, "DEBIT", 100_000, "091", "VND"),
		},
	}
	if err := validateManualPostingFlow(FlowOffBalance, validDebit); err != nil {
		t.Fatalf("valid off-balance debit rejected: %v", err)
	}
	validCredit := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines:          []*financev1.PostingLine{line(1, "CREDIT", 500, "092", "VND")},
	}
	if err := validateManualPostingFlow(FlowOffBalance, validCredit); err != nil {
		t.Fatalf("valid off-balance single credit line rejected: %v", err)
	}

	// Mixed directions are the same-line memo rule violation.
	mixed := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 100_000, "091", "VND"),
			line(2, "CREDIT", 100_000, "092", "VND"),
		},
	}
	if err := validateManualPostingFlow(FlowOffBalance, mixed); err == nil || !strings.Contains(err.Error(), "one direction") {
		t.Fatalf("mixed-direction off-balance error = %v", err)
	}

	unequal := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 100_000, "091", "VND"),
			line(2, "DEBIT", 90_000, "092", "VND"),
		},
	}
	if err := validateManualPostingFlow(FlowOffBalance, unequal); err == nil || !strings.Contains(err.Error(), "equal amounts") {
		t.Fatalf("unequal off-balance amounts error = %v", err)
	}
}

func TestValidateCancellationShape(t *testing.T) {
	if err := validateCancellationShape(nil); err == nil || !strings.Contains(err.Error(), "cancellation_request is required") {
		t.Fatalf("nil cancellation error = %v", err)
	}
	if err := validateCancellationShape(&CancellationCaseInput{ReferenceEntryNo: "1"}); err == nil || !strings.Contains(err.Error(), "reason is required") {
		t.Fatalf("missing reason error = %v", err)
	}
	if err := validateCancellationShape(&CancellationCaseInput{Reason: "sai số liệu"}); err == nil || !strings.Contains(err.Error(), "reference_entry_no is required") {
		t.Fatalf("missing reference error = %v", err)
	}
	if err := validateCancellationShape(&CancellationCaseInput{ReferenceEntryNo: "42", Reason: "sai số liệu", AccountingDate: "09/2026"}); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("bad accounting date error = %v", err)
	}
	valid := &CancellationCaseInput{ReferenceEntryNo: "42", Reason: "sai số liệu", AccountingDate: "2026-09-08"}
	if err := validateCancellationShape(valid); err != nil {
		t.Fatalf("valid cancellation rejected: %v", err)
	}
}

func TestCancellationRequestVariablesSerialization(t *testing.T) {
	in := &CancellationCaseInput{
		ReferenceEntryNo: "42",
		Reason:           "hủy sai số liệu",
		AccountingDate:   "2026-09-08",
		IdempotencyKey:   "fin-cancellation-abc",
		Trader: &CancellationTrader{
			ObjectType: "EMPLOYEE",
			ObjectCode: "NV001",
			ObjectName: "Nguyễn Văn A",
		},
	}
	vars := cancellationRequestVariables(in)
	if vars["referenceEntryNo"] != "42" || vars["reason"] != "hủy sai số liệu" || vars["idempotencyKey"] != "fin-cancellation-abc" {
		t.Fatalf("cancellation variables mismatch: %v", vars)
	}
	trader, ok := vars["trader"].(map[string]any)
	if !ok || trader["objectType"] != "EMPLOYEE" || trader["objectCode"] != "NV001" || trader["objectName"] != "Nguyễn Văn A" {
		t.Fatalf("trader variables mismatch: %v", trader)
	}
	if _, ok := trader["address"]; ok {
		t.Fatal("empty trader fields must be omitted from case variables")
	}

	minimal := cancellationRequestVariables(&CancellationCaseInput{ReferenceEntryNo: "1", Reason: "r"})
	if _, ok := minimal["accountingDate"]; ok {
		t.Fatal("empty accountingDate must be omitted from case variables")
	}
	if _, ok := minimal["trader"]; ok {
		t.Fatal("nil trader must be omitted from case variables")
	}
}

func TestPostingRequestVariablesSerialization(t *testing.T) {
	req := &financev1.PostingRequest{
		IdempotencyKey: "fin-single-entry-abc",
		AccountingDate: "2026-09-08",
		CurrencyCode:   "VND",
		Description:    "Chi tiền mặt",
		Lines: []*financev1.PostingLine{
			{LineNo: 1, Direction: "DEBIT", AmountMinor: 250_000, AccountCode: "1111", CurrencyCode: "VND"},
			{LineNo: 2, Direction: "CREDIT", AmountMinor: 250_000, AccountCode: "5111", CoaVersion: "2024", CounterpartyCode: "NCC01", Description: "mua hàng"},
		},
	}
	vars := postingRequestVariables(req)
	if vars["idempotencyKey"] != "fin-single-entry-abc" || vars["accountingDate"] != "2026-09-08" {
		t.Fatalf("header variables mismatch: %v", vars)
	}
	lines := vars["lines"].([]any)
	if len(lines) != 2 {
		t.Fatalf("lines len = %d, want 2", len(lines))
	}
	first := lines[0].(map[string]any)
	if first["lineNo"] != int32(1) || first["direction"] != "DEBIT" || first["amountMinor"] != int64(250_000) || first["accountCode"] != "1111" {
		t.Fatalf("line 1 variables mismatch: %v", first)
	}
	second := lines[1].(map[string]any)
	if second["coaVersion"] != "2024" || second["counterpartyCode"] != "NCC01" || second["description"] != "mua hàng" {
		t.Fatalf("line 2 optional variables mismatch: %v", second)
	}
	if _, ok := first["coaVersion"]; ok {
		t.Fatal("empty optional fields must be omitted from case variables")
	}
}

func TestTruncateDescriptionRuneSafe(t *testing.T) {
	long := strings.Repeat("á", 100)
	got := truncateDescription(long)
	if got != strings.Repeat("á", 80) {
		t.Fatalf("truncateDescription cut = %d chars, want 80", len([]rune(got)))
	}
	if truncateDescription("  ngắn  ") != "ngắn" {
		t.Fatalf("truncateDescription trims = %q", truncateDescription("  ngắn  "))
	}
}

func TestValidateManualPostingFlowDoubleEntry(t *testing.T) {
	valid := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 1_000_000, "1111", "VND"),
			line(2, "CREDIT", 600_000, "5111", "VND"),
			line(3, "CREDIT", 400_000, "1311", "VND"),
		},
	}
	if err := validateManualPostingFlow(FlowDoubleEntry, valid); err != nil {
		t.Fatalf("valid double entry rejected: %v", err)
	}

	multiCurrency := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 100, "1111", "VND"),
			line(2, "CREDIT", 100, "5111", "VND"),
			line(3, "DEBIT", 50, "1111", "USD"),
			line(4, "CREDIT", 50, "5111", "USD"),
		},
	}
	if err := validateManualPostingFlow(FlowDoubleEntry, multiCurrency); err != nil {
		t.Fatalf("balanced multi-currency double entry rejected: %v", err)
	}

	if err := validateManualPostingFlow(FlowDoubleEntry, &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines:          []*financev1.PostingLine{line(1, "DEBIT", 100, "1111", "VND")},
	}); err == nil || !strings.Contains(err.Error(), "at least 2 lines") {
		t.Fatalf("one-line double entry error = %v", err)
	}

	unbalanced := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 1_000_000, "1111", "VND"),
			line(2, "CREDIT", 999_000, "5111", "VND"),
		},
	}
	err := validateManualPostingFlow(FlowDoubleEntry, unbalanced)
	if err == nil || !strings.Contains(err.Error(), "unbalanced for VND") {
		t.Fatalf("unbalanced double entry error = %v", err)
	}

	// VND balances but USD does not — the per-currency rule must fire.
	partiallyUnbalanced := &financev1.PostingRequest{
		AccountingDate: "2026-09-08",
		Lines: []*financev1.PostingLine{
			line(1, "DEBIT", 100, "1111", "VND"),
			line(2, "CREDIT", 100, "5111", "VND"),
			line(3, "DEBIT", 50, "1111", "USD"),
			line(4, "CREDIT", 40, "5111", "USD"),
		},
	}
	err = validateManualPostingFlow(FlowDoubleEntry, partiallyUnbalanced)
	if err == nil || !strings.Contains(err.Error(), "unbalanced for USD") {
		t.Fatalf("partially unbalanced double entry error = %v", err)
	}
}
