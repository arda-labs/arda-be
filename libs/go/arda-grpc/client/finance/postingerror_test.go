package finance

import (
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAsPostingError(t *testing.T) {
	st := status.New(codes.FailedPrecondition, "BAL_AVAILABLE_IS_NOT_ENOUGH:1111")
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: "POSTING_ERROR",
		Metadata: map[string]string{
			"posting_error_code": financev1.PostingErrorCode_POSTING_ERROR_CODE_INSUFFICIENT_BALANCE.String(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := AsPostingError(withDetails.Err())
	if !ok || got.Code != financev1.PostingErrorCode_POSTING_ERROR_CODE_INSUFFICIENT_BALANCE {
		t.Fatalf("AsPostingError() = %#v, %t", got, ok)
	}
	if got.Error() != "BAL_AVAILABLE_IS_NOT_ENOUGH:1111" {
		t.Fatalf("message = %q", got.Error())
	}
}

func TestAsPostingErrorLeavesLegacyAndTransientErrorsUntyped(t *testing.T) {
	for _, err := range []error{status.Error(codes.FailedPrecondition, "PERIOD_CLOSED"), status.Error(codes.Unavailable, "offline")} {
		if _, ok := AsPostingError(err); ok {
			t.Fatalf("legacy error %v was unexpectedly typed", err)
		}
	}
}
