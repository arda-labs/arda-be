package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestValidateClosingShape(t *testing.T) {
	valid := &ClosingCaseInput{
		AccountingDate: "2026-09-30",
		PeriodType:     "M",
		Rows: []ClosingCaseRow{
			{AccCode: "5111", AccPurpose: "INC", AmountMinor: 500_000},
			{AccCode: "6321", AccPurpose: "EXP", AmountMinor: 200_000},
		},
	}
	if err := validateClosingShape(valid); err != nil {
		t.Fatalf("valid closing rejected: %v", err)
	}
	if valid.PeriodType != "M" {
		t.Fatalf("period_type must not be mutated, got %q", valid.PeriodType)
	}

	// Empty period type defaults to Y (kỳ năm).
	yearly := &ClosingCaseInput{
		AccountingDate: "2026-12-31",
		Rows:           []ClosingCaseRow{{AccCode: "5111", AccPurpose: "INC", AmountMinor: 1}},
	}
	if err := validateClosingShape(yearly); err != nil {
		t.Fatalf("default period_type rejected: %v", err)
	}
	if yearly.PeriodType != "Y" {
		t.Fatalf("period_type default = %q, want Y", yearly.PeriodType)
	}

	if err := validateClosingShape(nil); err == nil || !strings.Contains(err.Error(), "closing_request is required") {
		t.Fatalf("nil input error = %v", err)
	}
	if err := validateClosingShape(&ClosingCaseInput{Rows: []ClosingCaseRow{{AccCode: "5111", AccPurpose: "INC", AmountMinor: 1}}}); err == nil || !strings.Contains(err.Error(), "accounting_date") {
		t.Fatalf("missing date error = %v", err)
	}
	if err := validateClosingShape(&ClosingCaseInput{AccountingDate: "30-09-2026", Rows: []ClosingCaseRow{{AccCode: "5111", AccPurpose: "INC", AmountMinor: 1}}}); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("bad date error = %v", err)
	}
	if err := validateClosingShape(&ClosingCaseInput{AccountingDate: "2026-09-30", PeriodType: "W", Rows: []ClosingCaseRow{{AccCode: "5111", AccPurpose: "INC", AmountMinor: 1}}}); err == nil || !strings.Contains(err.Error(), "period_type") {
		t.Fatalf("bad period_type error = %v", err)
	}
	if err := validateClosingShape(&ClosingCaseInput{AccountingDate: "2026-09-30"}); err == nil || !strings.Contains(err.Error(), "rows must not be empty") {
		t.Fatalf("empty rows error = %v", err)
	}
	if err := validateClosingShape(&ClosingCaseInput{AccountingDate: "2026-09-30", Rows: []ClosingCaseRow{{AccCode: "5111", AccPurpose: "REVENUE", AmountMinor: 1}}}); err == nil || !strings.Contains(err.Error(), "INC or EXP") {
		t.Fatalf("bad purpose error = %v", err)
	}
	if err := validateClosingShape(&ClosingCaseInput{AccountingDate: "2026-09-30", Rows: []ClosingCaseRow{{AccCode: "5111", AccPurpose: "INC", AmountMinor: 0}}}); err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("non-positive amount error = %v", err)
	}
	if err := validateClosingShape(&ClosingCaseInput{AccountingDate: "2026-09-30", Rows: []ClosingCaseRow{{AccCode: "", AccPurpose: "INC", AmountMinor: 1}}}); err == nil || !strings.Contains(err.Error(), "acc_code is required") {
		t.Fatalf("missing acc_code error = %v", err)
	}
}

// TestCheckPostingDateFutureDate covers the repo-free policy paths (the
// backdate branches need a policy row from the DB — GATE smoke territory).
func TestCheckPostingDateFutureDate(t *testing.T) {
	svc := &PostingService{}
	now := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	violation, err := svc.checkPostingDate(nil, "tenant", "FIN_SINGLE_ENTRY", "2026-09-09", now)
	if err != nil {
		t.Fatalf("future date check infra error = %v", err)
	}
	if violation == nil || !errors.Is(violation, ErrTransactionDateExceedsCurrentDate) {
		t.Fatalf("future date violation = %v, want ErrTransactionDateExceedsCurrentDate", violation)
	}

	// Today passes; an unparseable date is left to the caller's validation.
	for _, date := range []string{"2026-09-08", "", "   ", "not-a-date"} {
		violation, err := svc.checkPostingDate(nil, "tenant", "FIN_SINGLE_ENTRY", date, now)
		if err != nil || violation != nil {
			t.Fatalf("date %q → (%v, %v), want no violation", date, violation, err)
		}
	}
}

func TestPostingPolicyErrorMessagesCarrySentinel(t *testing.T) {
	err := fmt.Errorf("%w: detail", ErrBackdateNotAllowed)
	if !errors.Is(err, ErrBackdateNotAllowed) {
		t.Fatal("wrapped sentinel must satisfy errors.Is")
	}
	// The sentinel is the stable code the FE/workers key on.
	if !strings.HasPrefix(ErrTransactionDateExceedsBackdate.Error(), "TRANSACTION_DATE_") {
		t.Fatalf("unexpected sentinel code %q", ErrTransactionDateExceedsBackdate.Error())
	}
}
