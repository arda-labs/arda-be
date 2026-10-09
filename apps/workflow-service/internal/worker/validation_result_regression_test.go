package worker

import (
	"errors"
	"testing"

	financev1 "github.com/arda-labs/arda/libs/go/arda-proto/finance/v1"
)

// Exercise the routing currently used by manual validate for Valid=false.
func TestInvalidValidationResultReturnsToMaker(t *testing.T) {
	for _, message := range []string{"NO_LINES", "INVALID_DIRECTION", "CLASSIFICATION_REQUIRED", "UNKNOWN_DIMENSION:branch", "line 1: account is missing"} {
		t.Run(message, func(t *testing.T) {
			result := &financev1.ValidationResult{Valid: false, GlobalErrors: []string{message}}
			released, business, transient := 0, 0, 0
			routeInvalidValidation(result, "pending-id",
				func() error { released++; return nil },
				func(financev1.PostingErrorCode, string) { business++ },
				func(error) { transient++ })
			if released != 1 || business != 1 || transient != 0 {
				t.Fatalf("invalid result routed as released/business/transient=%d/%d/%d; want 1/1/0", released, business, transient)
			}
		})
	}
}

func TestInvalidValidationReleaseFailureRetries(t *testing.T) {
	business, transient := 0, 0
	routeInvalidValidation(&financev1.ValidationResult{Valid: false}, "pending-id",
		func() error { return errors.New("unavailable") },
		func(financev1.PostingErrorCode, string) { business++ },
		func(error) { transient++ })
	if business != 0 || transient != 1 {
		t.Fatalf("business/transient=%d/%d; want 0/1", business, transient)
	}
}

func TestInvalidValidationWithoutHoldReturnsToMaker(t *testing.T) {
	business := 0
	routeInvalidValidation(&financev1.ValidationResult{Valid: false}, "", nil,
		func(_ financev1.PostingErrorCode, message string) {
			business++
			if message == "" {
				t.Fatal("missing validation reason")
			}
		}, func(err error) { t.Fatalf("unexpected retry: %v", err) })
	if business != 1 {
		t.Fatalf("business=%d; want 1", business)
	}
}
