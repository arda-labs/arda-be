package worker

import (
	"errors"
	"strings"
	"testing"
)

func TestPostingRequestFromVarsBuildsFinanceRequest(t *testing.T) {
	vars := map[string]any{
		"caseId":                "case-123",
		"postingIdempotencyKey": "fin-single-entry-abc",
		"postingRequest": map[string]any{
			"idempotencyKey": "ignored-in-favour-of-case-var",
			"accountingDate": "2026-09-08",
			"currencyCode":   "VND",
			"description":    "Chi tiền mặt",
			"lines": []any{
				map[string]any{
					"lineNo":       float64(1),
					"direction":    "DEBIT",
					"amountMinor":  float64(250000),
					"accountCode":  "1111",
					"currencyCode": "VND",
				},
				map[string]any{
					"direction":   "CREDIT",
					"amountMinor": float64(250000),
					"accountCode": "5111",
				},
			},
		},
	}
	req, err := postingRequestFromVars(vars, SingleEntryFlow)
	if err != nil {
		t.Fatalf("postingRequestFromVars() error = %v", err)
	}
	if req.GetIdempotencyKey() != "fin-single-entry-abc" {
		t.Fatalf("idempotency key = %q, want case-variable key", req.GetIdempotencyKey())
	}
	if req.GetAccountingDate() != "2026-09-08" || req.GetCurrencyCode() != "VND" || req.GetDescription() != "Chi tiền mặt" {
		t.Fatalf("request header mismatch: %+v", req)
	}
	ref := req.GetBusinessReference()
	if ref.GetDomain() != "fin" || ref.GetDocumentType() != "FIN_SINGLE_ENTRY" || ref.GetCaseId() != "case-123" {
		t.Fatalf("business reference mismatch: %+v", ref)
	}
	if len(req.GetLines()) != 2 {
		t.Fatalf("lines len = %d, want 2", len(req.GetLines()))
	}
	first, second := req.GetLines()[0], req.GetLines()[1]
	if first.GetLineNo() != 1 || first.GetDirection() != "DEBIT" || first.GetAmountMinor() != 250000 || first.GetAccountCode() != "1111" {
		t.Fatalf("line 1 mismatch: %+v", first)
	}
	// Positional numbering fills in when the maker dropped lineNo.
	if second.GetLineNo() != 2 || second.GetDirection() != "CREDIT" || second.GetAmountMinor() != 250000 || second.GetAccountCode() != "5111" {
		t.Fatalf("line 2 mismatch: %+v", second)
	}
}

func TestPostingRequestFromVarsDoubleEntryDocumentType(t *testing.T) {
	vars := map[string]any{
		"postingIdempotencyKey": "fin-double-entry-xyz",
		"postingRequest": map[string]any{
			"accountingDate": "2026-09-08",
			"lines": []any{
				map[string]any{"direction": "DEBIT", "amountMinor": float64(10), "accountCode": "1111"},
				map[string]any{"direction": "CREDIT", "amountMinor": float64(10), "accountCode": "5111"},
			},
		},
	}
	req, err := postingRequestFromVars(vars, DoubleEntryFlow)
	if err != nil {
		t.Fatalf("postingRequestFromVars() error = %v", err)
	}
	if req.GetBusinessReference().GetDocumentType() != "FIN_DOUBLE_ENTRY" {
		t.Fatalf("document type = %q, want FIN_DOUBLE_ENTRY", req.GetBusinessReference().GetDocumentType())
	}
}

func TestPostingRequestFromVarsRejectsBrokenVariables(t *testing.T) {
	if _, err := postingRequestFromVars(map[string]any{}, SingleEntryFlow); err == nil || !strings.Contains(err.Error(), "postingIdempotencyKey") {
		t.Fatalf("missing idempotency key error = %v", err)
	}
	if _, err := postingRequestFromVars(map[string]any{"postingIdempotencyKey": "k"}, SingleEntryFlow); err == nil || !strings.Contains(err.Error(), "postingRequest") {
		t.Fatalf("missing postingRequest error = %v", err)
	}
	if _, err := postingRequestFromVars(map[string]any{
		"postingIdempotencyKey": "k",
		"postingRequest":        map[string]any{"lines": []any{}},
	}, SingleEntryFlow); err == nil || !strings.Contains(err.Error(), "lines must not be empty") {
		t.Fatalf("empty lines error = %v", err)
	}
}

func TestClosingFlowBuildsFinanceRequest(t *testing.T) {
	// The closing case variables carry the server-built postingRequest
	// (finance-service constructed the balanced lines from the maker's
	// INC/EXP rows) — the worker mirrors the manual posting deserialization
	// with the FIN_CLOSING document type.
	vars := map[string]any{
		"caseId":                "case-777",
		"postingIdempotencyKey": "fin-closing-abc",
		"postingRequest": map[string]any{
			"accountingDate": "2026-09-08",
			"currencyCode":   "VND",
			"description":    "Kết chuyển thu chi kỳ Y — 2026-09-08",
			"lines": []any{
				map[string]any{"lineNo": float64(1), "direction": "DEBIT", "amountMinor": float64(500000), "accountCode": "5111", "coaVersion": "V1"},
				map[string]any{"lineNo": float64(2), "direction": "CREDIT", "amountMinor": float64(500000), "accountCode": "4211", "coaVersion": "V1"},
			},
		},
	}
	req, err := postingRequestFromVars(vars, ClosingFlow)
	if err != nil {
		t.Fatalf("postingRequestFromVars(closing) error = %v", err)
	}
	if req.GetIdempotencyKey() != "fin-closing-abc" {
		t.Fatalf("idempotency key = %q, want fin-closing-abc", req.GetIdempotencyKey())
	}
	ref := req.GetBusinessReference()
	if ref.GetDomain() != "fin" || ref.GetDocumentType() != "FIN_CLOSING" || ref.GetCaseId() != "case-777" {
		t.Fatalf("business reference mismatch: %+v", ref)
	}
	if len(req.GetLines()) != 2 || req.GetLines()[0].GetAccountCode() != "5111" || req.GetLines()[1].GetAccountCode() != "4211" {
		t.Fatalf("closing lines mismatch: %+v", req.GetLines())
	}
}

func TestIsPostingPolicyError(t *testing.T) {
	// Finance-service stamps the sentinel code at the start of the message;
	// the gRPC layer prefixes transport detail, so containment decides.
	policyMsg := errors.New("rpc error: code = FailedPrecondition desc = TRANSACTION_DATE_EXCEEDS_BACKDATE: accounting date 2026-01-01 is 250 days back")
	if !isPostingPolicyError(policyMsg) {
		t.Fatal("policy sentinel message must classify as validation")
	}
	infraMsg := errors.New("rpc error: code = Unavailable desc = connection refused")
	if isPostingPolicyError(infraMsg) {
		t.Fatal("infra failure must not classify as validation")
	}
	for _, sentinel := range []string{"TRANSACTION_DATE_EXCEEDS_CURRENT_DATE", "BACKDATE_NOT_ALLOWED", "TRANSACTION_DATE_EXCEEDS_BACKDATE", "POSTING_DATE_BEFORE_CLOSING_LOCK"} {
		if !isPostingPolicyError(errors.New("desc = " + sentinel + ": detail")) {
			t.Fatalf("sentinel %s must classify as validation", sentinel)
		}
	}
}

func TestPostingRequestFromVarsMergesTraderStamp(t *testing.T) {
	// The closing case carries the trader block inside closingMeta; the
	// worker must merge the fixed trader_* keys into the request metadata
	// while keeping the posting request itself intact.
	vars := map[string]any{
		"postingIdempotencyKey": "fin-closing-trader",
		"closingMeta": map[string]any{
			"trader": map[string]any{
				"objectType": "CUST",
				"objectCode": "KH001",
				"objectName": "  Nguyễn Văn A  ",
				"idNumber":   "012345678",
			},
		},
		"postingRequest": map[string]any{
			"accountingDate": "2026-09-09",
			"lines": []any{
				map[string]any{"direction": "DEBIT", "amountMinor": float64(10), "accountCode": "1111"},
				map[string]any{"direction": "CREDIT", "amountMinor": float64(10), "accountCode": "5111"},
			},
		},
	}
	req, err := postingRequestFromVars(vars, ClosingFlow)
	if err != nil {
		t.Fatalf("postingRequestFromVars() error = %v", err)
	}
	meta := req.GetMetadata()
	if meta["trader_object_type"] != "CUST" || meta["trader_object_code"] != "KH001" ||
		meta["trader_object_name"] != "Nguyễn Văn A" || meta["trader_id_number"] != "012345678" {
		t.Fatalf("trader metadata mismatch: %v", meta)
	}
	// Only keys with a value are stamped.
	if _, ok := meta["trader_address"]; ok {
		t.Fatalf("empty trader keys must not be stamped: %v", meta)
	}
}

func TestTraderStampFromVarsWithoutTrader(t *testing.T) {
	if got := traderStampFromVars(map[string]any{"postingIdempotencyKey": "k"}); got != nil {
		t.Fatalf("no trader block = %v, want nil", got)
	}
	// A trader object with every field empty stamps nothing.
	if got := traderStampFromVars(map[string]any{"trader": map[string]any{"objectType": ""}}); got != nil {
		t.Fatalf("empty trader block = %v, want nil", got)
	}
}
