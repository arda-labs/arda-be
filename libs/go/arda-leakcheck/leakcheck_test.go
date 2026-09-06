package ardaleakcheck

import (
	"runtime"
	"strings"
	"testing"
)

// TestProfileCountsProvenLeak leaks a goroutine on a channel that nothing
// (else) references, then verifies the Go 1.27 leak profile proves it leaked.
func TestProfileCountsProvenLeak(t *testing.T) {
	ch := make(chan int)
	go func() { <-ch }()
	ch = nil
	runtime.GC()

	n, dump, err := profile()
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 leaked goroutine, got %d:\n%s", n, dump)
	}
	if !strings.Contains(dump, "goroutineleak profile: total") {
		t.Fatalf("unexpected profile text:\n%s", dump)
	}
}
