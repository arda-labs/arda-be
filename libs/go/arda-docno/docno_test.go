package docno

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func testSeries(reset ResetPeriod, pattern string, width int) Series {
	return Series{ID: "series-1", TenantID: "tenant-1", DocumentType: "CASH_IN", Version: 1,
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ResetPeriod: reset,
		Pattern: pattern, SequenceStart: 1, SequenceWidth: width, OverflowPolicy: "REJECT", Status: "ACTIVE"}
}

func TestPeriodKeyUsesExplicitBusinessDate(t *testing.T) {
	date := time.Date(2026, 10, 9, 23, 0, 0, 0, time.FixedZone("UTC+7", 7*60*60))
	for _, tc := range []struct {
		reset ResetPeriod
		want  string
	}{{ResetNone, "ALL"}, {ResetYear, "2026"}, {ResetMonth, "2026-10"}, {ResetDay, "2026-10-09"}} {
		got, err := PeriodKey(tc.reset, date)
		if err != nil || got != tc.want {
			t.Fatalf("PeriodKey(%s) = %q, %v; want %q", tc.reset, got, err, tc.want)
		}
	}
}

func TestFormatDisplayNumberIncludesPeriodTokensAndDoesNotTruncate(t *testing.T) {
	date := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	series := testSeries(ResetDay, "RC-{YYYY}{MM}{DD}-{SEQ:3}", 3)
	got, err := FormatDisplayNumber(series, date, 12)
	if err != nil || got != "RC-20261009-012" {
		t.Fatalf("FormatDisplayNumber() = %q, %v", got, err)
	}
	if _, err := FormatDisplayNumber(series, date, 1000); !errors.Is(err, ErrSequenceOverflow) {
		t.Fatalf("overflow error = %v; want %v", err, ErrSequenceOverflow)
	}
}

func TestValidateSeriesRequiresPeriodTokens(t *testing.T) {
	if err := ValidateSeries(testSeries(ResetDay, "RC-{SEQ:4}", 4)); !errors.Is(err, ErrInvalidSeries) {
		t.Fatalf("ValidateSeries() = %v; want invalid series", err)
	}
}

func TestResolveRuleNeedsExplicitGlobalFallbackAndUsesPriority(t *testing.T) {
	selector := RuleSelector{TenantID: "tenant-1", OrgCode: "BR-1", Ledger: "CASH", PaymentMethod: "CASH", EntryDirection: "IN"}
	rules := []DocumentTypeRule{{Ledger: "CASH", PaymentMethod: "CASH", EntryDirection: "IN", DocumentType: "GLOBAL", Priority: 3}}
	if _, err := ResolveDocumentTypeRule(selector, rules, false, nil); !errors.Is(err, ErrDocTypeRuleNotFound) {
		t.Fatalf("implicit fallback error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	selected, err := ResolveDocumentTypeRule(selector, rules, true, logger)
	if err != nil || selected.DocumentType != "GLOBAL" {
		t.Fatalf("explicit fallback selected %#v, %v", selected, err)
	}
	rules = append(rules, DocumentTypeRule{OrgCode: "BR-1", Ledger: "CASH", PaymentMethod: "CASH", EntryDirection: "IN", DocumentType: "ORG-LOW", Priority: 9}, DocumentTypeRule{OrgCode: "BR-1", Ledger: "CASH", PaymentMethod: "CASH", EntryDirection: "IN", DocumentType: "ORG-HIGH", Priority: 1})
	selected, err = ResolveDocumentTypeRule(selector, rules, true, logger)
	if err != nil || selected.DocumentType != "ORG-HIGH" {
		t.Fatalf("org-specific rule selected %#v, %v", selected, err)
	}
}
