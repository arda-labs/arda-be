package worker

import (
	"errors"
	"testing"
)

func TestPostingClassificationRequiresTypedContract(t *testing.T) {
	for _, message := range []string{
		"database unavailable while reading PERIOD_CLOSED audit record",
		"transport failure for INVALID_AMOUNT metric",
		"UNBALANCED and INSUFFICIENT_BALANCE diagnostic labels",
	} {
		if code, business := classifyPostingError(errors.New(message)); business {
			t.Errorf("untyped error %q classified as business %s", message, code)
		}
	}
}
