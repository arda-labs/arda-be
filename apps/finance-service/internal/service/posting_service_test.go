package service

import (
	"encoding/json"
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// Unit tests for the pure parts of the posting flow. DB paths (COA resolve,
// idempotency replay, outbox) are covered by the GATE smoke run.

func TestAnalyticsJSONContainsFlatKeys(t *testing.T) {
	got := analyticsJSON(&financev1.Analytics{AccClassification: "X"})
	var parsed map[string]string
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("analytics JSON invalid: %v", err)
	}
	for _, key := range []string{"acc_classification", "debt_group_code", "org_unit_code", "contract_code"} {
		if _, ok := parsed[key]; !ok {
			t.Fatalf("analytics JSON missing %q: %s", key, got)
		}
	}
	if _, ok := parsed["unknown_key"]; ok {
		t.Fatal("dimension extras must be flattened, not nested")
	}
}

func TestAnalyticsJSONNil(t *testing.T) {
	if string(analyticsJSON(nil)) != "{}" {
		t.Fatal("nil analytics must encode to {}")
	}
}

func TestOutboxPayloadShape(t *testing.T) {
	req := &financev1.PostingRequest{
		AccountingDate: "2026-09-07",
		CurrencyCode:   "VND",
		BusinessReference: &financev1.BusinessReference{
			Domain:       "lnm",
			DocumentType: "LNM_DISBURSEMENT",
			DocumentId:   "11111111-1111-1111-1111-111111111111",
		},
		Lines: []*financev1.PostingLine{
			{Direction: "DEBIT", AmountMinor: 100},
			{Direction: "CREDIT", AmountMinor: 100},
		},
	}
	var parsed map[string]any
	if err := json.Unmarshal(outboxPayload("entry-1", req), &parsed); err != nil {
		t.Fatalf("outbox payload invalid: %v", err)
	}
	if parsed["entry_id"] != "entry-1" || parsed["debit_minor"] != float64(100) || parsed["credit_minor"] != float64(100) {
		t.Fatalf("outbox payload wrong: %s", outboxPayload("entry-1", req))
	}
}
