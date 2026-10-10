package service

import (
	"errors"
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

func TestPostingClassificationDoesNotParseInfrastructureMessage(t *testing.T) {
	for _, message := range []string{
		"database unavailable while reading PERIOD_CLOSED audit record",
		"transport failure for INVALID_AMOUNT metric",
		"UNBALANCED and INSUFFICIENT_BALANCE diagnostic labels",
	} {
		if got := ClassifyPostingError(errors.New(message)); got != financev1.PostingErrorCode_POSTING_ERROR_CODE_UNSPECIFIED {
			t.Errorf("infrastructure error %q classified as %s", message, got)
		}
	}
}
