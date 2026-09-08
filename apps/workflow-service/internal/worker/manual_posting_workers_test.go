package worker

import (
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
