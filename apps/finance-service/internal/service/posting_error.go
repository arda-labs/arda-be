package service

import (
	"errors"
	"strings"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// ClassifyPostingError maps stable finance policy/validation errors to the
// gRPC contract enum. Unrecognized errors remain unspecified and retryable.
func ClassifyPostingError(err error) financev1.PostingErrorCode {
	if err == nil {
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_UNSPECIFIED
	}
	message := strings.ToUpper(err.Error())
	switch {
	case strings.Contains(message, "UNBALANCED"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_UNBALANCED
	case strings.Contains(message, "PERIOD_CLOSED"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED
	case errors.Is(err, ErrBackdateNotAllowed), strings.Contains(message, "BACKDATE_NOT_ALLOWED"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_BACKDATE_NOT_ALLOWED
	case errors.Is(err, ErrTransactionDateExceedsCurrentDate), strings.Contains(message, "TRANSACTION_DATE_EXCEEDS_CURRENT_DATE"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_CURRENT_DATE
	case errors.Is(err, ErrTransactionDateExceedsBackdate), strings.Contains(message, "TRANSACTION_DATE_EXCEEDS_BACKDATE"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_BACKDATE
	case errors.Is(err, ErrPostingDateBeforeClosingLock), strings.Contains(message, "POSTING_DATE_BEFORE_CLOSING_LOCK"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_BEFORE_CLOSING_LOCK
	case strings.Contains(message, "BAL_AVAILABLE_IS_NOT_ENOUGH"), strings.Contains(message, "BAL_ACTUAL_IS_NOT_ENOUGH"), strings.Contains(message, "INSUFFICIENT_BALANCE"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_INSUFFICIENT_BALANCE
	case strings.Contains(message, "ACCOUNT_UNRESOLVED"), strings.Contains(message, "ACCOUNT_NOT_FOUND"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED
	case strings.Contains(message, "RULE_NOT_FOUND"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND
	case strings.Contains(message, "DUPLICATE_IDEMPOTENCY_KEY_CONFLICT"), strings.Contains(message, "IDEMPOTENCY KEY"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_DUPLICATE_IDEMPOTENCY_KEY_CONFLICT
	case strings.Contains(message, "PERIOD_NOT_FOUND"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_NOT_FOUND
	case strings.Contains(message, "INVALID_AMOUNT"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_INVALID_AMOUNT
	case strings.Contains(message, "CURRENCY_MISMATCH"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_CURRENCY_MISMATCH
	case strings.Contains(message, "CONCURRENT_MODIFICATION"):
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_CONCURRENT_MODIFICATION
	default:
		return financev1.PostingErrorCode_POSTING_ERROR_CODE_UNSPECIFIED
	}
}
