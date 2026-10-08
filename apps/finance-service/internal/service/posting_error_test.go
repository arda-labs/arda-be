package service

import (
	"errors"
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

func TestClassifyPostingError(t *testing.T) {
	tests := []struct {
		message string
		want    financev1.PostingErrorCode
	}{
		{"posting rejected: UNBALANCED:VND", financev1.PostingErrorCode_POSTING_ERROR_CODE_UNBALANCED},
		{"PERIOD_CLOSED: period closed", financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED},
		{"BACKDATE_NOT_ALLOWED: policy", financev1.PostingErrorCode_POSTING_ERROR_CODE_BACKDATE_NOT_ALLOWED},
		{"BAL_AVAILABLE_IS_NOT_ENOUGH:1111", financev1.PostingErrorCode_POSTING_ERROR_CODE_INSUFFICIENT_BALANCE},
		{"BAL_ACTUAL_IS_NOT_ENOUGH:1111", financev1.PostingErrorCode_POSTING_ERROR_CODE_INSUFFICIENT_BALANCE},
		{"posting rejected: line 1: ACCOUNT_UNRESOLVED", financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED},
		{"RULE_NOT_FOUND: LNM_COLLECTION", financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND},
		{"idempotency key content conflict", financev1.PostingErrorCode_POSTING_ERROR_CODE_DUPLICATE_IDEMPOTENCY_KEY_CONFLICT},
		{"PERIOD_NOT_FOUND", financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_NOT_FOUND},
		{"INVALID_AMOUNT", financev1.PostingErrorCode_POSTING_ERROR_CODE_INVALID_AMOUNT},
		{"CURRENCY_MISMATCH", financev1.PostingErrorCode_POSTING_ERROR_CODE_CURRENCY_MISMATCH},
		{"CONCURRENT_MODIFICATION", financev1.PostingErrorCode_POSTING_ERROR_CODE_CONCURRENT_MODIFICATION},
		{"TRANSACTION_DATE_EXCEEDS_CURRENT_DATE", financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_CURRENT_DATE},
		{"TRANSACTION_DATE_EXCEEDS_BACKDATE", financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_BACKDATE},
		{"POSTING_DATE_BEFORE_CLOSING_LOCK", financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_BEFORE_CLOSING_LOCK},
	}
	for _, tc := range tests {
		t.Run(tc.want.String(), func(t *testing.T) {
			got := ClassifyPostingError(errors.New(tc.message))
			if got != tc.want {
				t.Fatalf("ClassifyPostingError(%q) = %s, want %s", tc.message, got, tc.want)
			}
		})
	}
	if got := ClassifyPostingError(errors.New("database connection reset")); got != financev1.PostingErrorCode_POSTING_ERROR_CODE_UNSPECIFIED {
		t.Fatalf("transient error code = %s, want unspecified", got)
	}
}
