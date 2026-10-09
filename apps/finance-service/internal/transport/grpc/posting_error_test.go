package grpc

import (
	"errors"
	"testing"

	"github.com/arda-labs/arda/apps/finance-service/internal/service"
	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPostingErrorStatusAddsTypedDetailAndKeepsLegacyStatus(t *testing.T) {
	err := postingErrorStatus(errors.New("posting rejected: UNBALANCED:VND"))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("status = %s, want FailedPrecondition", status.Code(err))
	}
	if status.Convert(err).Message() != "posting rejected: UNBALANCED:VND" {
		t.Fatalf("legacy status message changed: %q", status.Convert(err).Message())
	}
	var found bool
	for _, detail := range status.Convert(err).Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if ok && info.GetReason() == "POSTING_ERROR" && info.GetMetadata()["posting_error_code"] == financev1.PostingErrorCode_POSTING_ERROR_CODE_UNBALANCED.String() {
			found = true
		}
	}
	if !found {
		t.Fatal("typed posting error detail missing")
	}
}

func TestPostingErrorStatusLeavesUnknownErrorUntyped(t *testing.T) {
	err := postingErrorStatus(errors.New("database connection reset"))
	if status.Code(err) != codes.FailedPrecondition || service.ClassifyPostingError(err) != financev1.PostingErrorCode_POSTING_ERROR_CODE_UNSPECIFIED {
		t.Fatalf("unknown error status/code = %s/%s", status.Code(err), service.ClassifyPostingError(err))
	}
	if len(status.Convert(err).Details()) != 0 {
		t.Fatal("unknown error should not gain a business posting detail")
	}
}

func TestValidationCodesCrossGRPCBoundary(t *testing.T) {
	for _, code := range []financev1.PostingErrorCode{
		financev1.PostingErrorCode_POSTING_ERROR_CODE_NO_LINES,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_INVALID_DIRECTION,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_CLASSIFICATION_REQUIRED,
		financev1.PostingErrorCode_POSTING_ERROR_CODE_UNKNOWN_DIMENSION,
	} {
		t.Run(code.String(), func(t *testing.T) {
			message := code.String()[len("POSTING_ERROR_CODE_"):]
			err := postingErrorStatus(errors.New("posting rejected: " + message))
			for _, detail := range status.Convert(err).Details() {
				if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetMetadata()["posting_error_code"] == code.String() {
					return
				}
			}
			t.Fatalf("missing typed validation code %s in %v", code, err)
		})
	}
}
