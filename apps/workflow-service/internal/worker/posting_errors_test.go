package worker

import (
	"errors"
	"testing"

	financeclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/finance"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

func TestRoutePostingFailureBusinessCodesReleaseHold(t *testing.T) {
	codes := []financev1.PostingErrorCode{
		financev1.PostingErrorCode_POSTING_ERROR_CODE_UNBALANCED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_BACKDATE_NOT_ALLOWED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_INSUFFICIENT_BALANCE,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_ACCOUNT_UNRESOLVED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_RULE_NOT_FOUND,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_DUPLICATE_IDEMPOTENCY_KEY_CONFLICT,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_NOT_FOUND,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_INVALID_AMOUNT,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_CURRENCY_MISMATCH,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_CURRENT_DATE,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_EXCEEDS_BACKDATE,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_POSTING_DATE_BEFORE_CLOSING_LOCK,
	}
	for _, code := range codes {
		t.Run(code.String(), func(t *testing.T) {
			released, business, transient := 0, 0, 0
			var got financev1.PostingErrorCode
			routePostingFailure(&financeclient.PostingError{Code: code, Cause: errors.New("finance detail")}, "pending-id",
				func() error { released++; return nil },
				func(actual financev1.PostingErrorCode, _ string) { business++; got = actual },
				func(error) { transient++ })
			if released != 1 || business != 1 || transient != 0 || got != code {
				t.Fatalf("released/business/transient/code = %d/%d/%d/%s", released, business, transient, got)
			}
		})
	}
}

func TestConcurrentModificationRemainsTransient(t *testing.T) {
	code := financev1.PostingErrorCode_POSTING_ERROR_CODE_CONCURRENT_MODIFICATION
	if isBusinessPostingErrorCode(code) {
		t.Fatal("concurrent modification can succeed on retry and must remain transient")
	}
}

func TestRoutePostingFailureRetriesUnknownOrReleaseFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		err         error
		releaseErr  error
		wantRelease int
	}{
		{name: "unknown transient", err: errors.New("connection reset")},
		{name: "failed release", err: &financeclient.PostingError{Code: financev1.PostingErrorCode_POSTING_ERROR_CODE_PERIOD_CLOSED, Cause: errors.New("closed")}, releaseErr: errors.New("finance unavailable"), wantRelease: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			released, business, transient := 0, 0, 0
			routePostingFailure(tc.err, "pending-id", func() error {
				released++
				return tc.releaseErr
			}, func(financev1.PostingErrorCode, string) { business++ }, func(error) { transient++ })
			if released != tc.wantRelease || business != 0 || transient != 1 {
				t.Fatalf("released/business/transient = %d/%d/%d", released, business, transient)
			}
		})
	}
}

func TestClassifyPostingErrorSupportsLegacyPolicyMessages(t *testing.T) {
	code, business := classifyPostingError(errors.New("rpc error: code = FailedPrecondition desc = BACKDATE_NOT_ALLOWED: detail"))
	if !business || code != financev1.PostingErrorCode_POSTING_ERROR_CODE_BACKDATE_NOT_ALLOWED {
		t.Fatalf("legacy classification = %s/%t", code, business)
	}
	if _, business := classifyPostingError(errors.New("rpc error: code = Unavailable desc = connection refused")); business {
		t.Fatal("transport failure must remain transient")
	}
}
