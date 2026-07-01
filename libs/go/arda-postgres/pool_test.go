package ardapostgres

import (
	"testing"
	"time"
)

func TestPoolOptionHelpers(t *testing.T) {
	t.Setenv("ARDA_TEST_INT", "12")
	if got := envInt("ARDA_TEST_INT", 8); got != 12 {
		t.Fatalf("envInt = %d, want 12", got)
	}
	t.Setenv("ARDA_TEST_INT", "nope")
	if got := envInt("ARDA_TEST_INT", 8); got != 8 {
		t.Fatalf("invalid envInt = %d, want fallback", got)
	}
	if got := firstPositive(0, 8); got != 8 {
		t.Fatalf("firstPositive zero = %d, want fallback", got)
	}
	if got := firstPositiveDuration(0, 5*time.Second); got != 5*time.Second {
		t.Fatalf("firstPositiveDuration zero = %s, want fallback", got)
	}
}
