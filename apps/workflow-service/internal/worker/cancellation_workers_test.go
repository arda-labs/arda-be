package worker

import (
	"strings"
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

func TestCancellationRequestFromVarsBuildsLookup(t *testing.T) {
	vars := map[string]any{
		"caseId":                "case-77",
		"tenantId":              "tenant-1",
		"postingIdempotencyKey": "fin-cancellation-abc",
		"cancellationRequest": map[string]any{
			"referenceEntryNo": "42",
			"reason":           "hủy sai số liệu",
			"accountingDate":   "2026-09-08",
		},
	}
	lookup, reason, accountingDate, idempotencyKey, err := cancellationRequestFromVars(vars)
	if err != nil {
		t.Fatalf("cancellationRequestFromVars() error = %v", err)
	}
	if lookup.GetEntryNo() != "42" || lookup.GetTenantId() != "tenant-1" {
		t.Fatalf("lookup mismatch: %+v", lookup)
	}
	if reason != "hủy sai số liệu" || accountingDate != "2026-09-08" {
		t.Fatalf("reason/date mismatch: %q %q", reason, accountingDate)
	}
	if idempotencyKey != "fin-cancellation-abc" {
		t.Fatalf("idempotency key = %q, want case-variable key", idempotencyKey)
	}
}

func TestCancellationRequestFromVarsRejectsBrokenVariables(t *testing.T) {
	if _, _, _, _, err := cancellationRequestFromVars(map[string]any{}); err == nil || !strings.Contains(err.Error(), "cancellationRequest") {
		t.Fatalf("missing cancellationRequest error = %v", err)
	}
	if _, _, _, _, err := cancellationRequestFromVars(map[string]any{
		"cancellationRequest": map[string]any{"reason": "x"},
	}); err == nil || !strings.Contains(err.Error(), "referenceEntryNo") {
		t.Fatalf("missing referenceEntryNo error = %v", err)
	}
	if _, _, _, _, err := cancellationRequestFromVars(map[string]any{
		"cancellationRequest": map[string]any{"referenceEntryNo": "42"},
	}); err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("missing reason error = %v", err)
	}
}

func TestGuardOriginalEntry(t *testing.T) {
	posted := &financev1.JournalEntryDetail{
		EntryNo:          42,
		Status:           "POSTED",
		TotalAmountMinor: 1000,
		Lines:            []*financev1.JournalEntryDetailLine{{LineNo: 1, Direction: "DEBIT", AmountMinor: 1000}},
	}
	if err := guardOriginalEntry(posted); err != nil {
		t.Fatalf("guardOriginalEntry(POSTED) error = %v", err)
	}

	reversed := &financev1.JournalEntryDetail{EntryNo: 42, Status: "POSTED", ReversedByEntryId: "re-1"}
	if err := guardOriginalEntry(reversed); err == nil || !strings.Contains(err.Error(), "already reversed") {
		t.Fatalf("guardOriginalEntry(already reversed) error = %v", err)
	}

	nonPosted := &financev1.JournalEntryDetail{EntryNo: 42, Status: "REVERSED"}
	if err := guardOriginalEntry(nonPosted); err == nil || !strings.Contains(err.Error(), "only POSTED") {
		t.Fatalf("guardOriginalEntry(REVERSED) error = %v", err)
	}

	noLines := &financev1.JournalEntryDetail{EntryNo: 42, Status: "POSTED"}
	if err := guardOriginalEntry(noLines); err == nil || !strings.Contains(err.Error(), "no lines") {
		t.Fatalf("guardOriginalEntry(no lines) error = %v", err)
	}
}

func TestCancellationFlowDocumentType(t *testing.T) {
	if TxnCancelFlow.DocumentType != "FIN_TXN_CANCEL" || TxnCancelFlow.TopicPrefix != "fin.txn-cancel" {
		t.Fatalf("TxnCancelFlow mismatch: %+v", TxnCancelFlow)
	}
	if OffBalanceFlow.DocumentType != "FIN_OFF_BALANCE" || OffBalanceFlow.TopicPrefix != "fin.off-balance" {
		t.Fatalf("OffBalanceFlow mismatch: %+v", OffBalanceFlow)
	}
}
