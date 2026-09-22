package catalog

import (
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// TestCoerceArgAcceptsEveryNumericFlavour locks the regression where a model
// sent limit:20 (a correct value) and got "limit must be a number": Goja hands
// integral JavaScript numbers to Go as int64, not float64.
func TestCoerceArgAcceptsEveryNumericFlavour(t *testing.T) {
	min, max := 1.0, 20.0
	arg := GeneratedArg{Name: "limit", Param: "per_page", Type: "integer", Min: &min, Max: &max}

	cases := []struct {
		name string
		raw  any
		want string
	}{
		{"float64", float64(20), "20"},
		{"int64 (goja)", int64(20), "20"},
		{"int", 20, "20"},
		{"int32", int32(20), "20"},
		{"float32", float32(20), "20"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := coerceArg(arg, tc.raw, true)
			if err != nil {
				t.Fatalf("coerceArg(%T) = %v, want %q", tc.raw, err, tc.want)
			}
			if got != tc.want {
				t.Fatalf("coerceArg(%T) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestCoerceArgStillRejectsBadValues keeps the guard rails after the widening:
// strings, out-of-range and non-integers must still fail.
func TestCoerceArgStillRejectsBadValues(t *testing.T) {
	min, max := 1.0, 20.0
	arg := GeneratedArg{Name: "limit", Param: "per_page", Type: "integer", Min: &min, Max: &max}

	if _, err := coerceArg(arg, "20", true); err == nil {
		t.Fatal("string numeric must still be rejected")
	}
	if _, err := coerceArg(arg, int64(100), true); err == nil || !strings.Contains(err.Error(), "must be <=") {
		t.Fatalf("out-of-range must fail with a bound error, got %v", err)
	}
	if _, err := coerceArg(arg, 2.5, true); err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("fractional integer must fail, got %v", err)
	}
}

// Compile-time link to the shared error sentinel used by the guard rails.
var _ = tools.ErrInvalidArgument
