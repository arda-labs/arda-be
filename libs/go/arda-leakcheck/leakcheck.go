// Package ardaleakcheck fails test runs whose goroutines are proven leaked by
// the Go 1.27 runtime goroutine leak detector.
package ardaleakcheck

import (
	"bytes"
	"fmt"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
)

// Run executes m like a TestMain body and, when every test passed, fails the
// process if the runtime goroutine leak profile reports leaked goroutines.
//
// The Go 1.27 goroutine leak detector marks a goroutine as leaked only when a
// GC cycle proves it is blocked on a concurrency primitive that is no longer
// reachable — meaning nothing can ever wake it. Intentional background
// goroutines (tickers, listeners, pools still referenced by the process) are
// not reported, so no allowlist is needed.
//
// Set ARDA_SKIP_LEAKCHECK=1 to bypass the check.
func Run(m *testing.M) int {
	code := m.Run()
	if code != 0 || os.Getenv("ARDA_SKIP_LEAKCHECK") == "1" {
		return code
	}
	leaked, dump, err := profile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ardaleakcheck: %v\n", err)
		return code
	}
	if leaked == 0 {
		return code
	}
	fmt.Fprintf(os.Stderr, "ardaleakcheck: FAIL: %d leaked goroutine(s) after tests\n%s", leaked, dump)
	return 1
}

// profile runs the leak-detection GC and returns the leaked-goroutine count
// with the full profile text for diagnosis.
func profile() (int, string, error) {
	p := pprof.Lookup("goroutineleak")
	if p == nil {
		return 0, "", fmt.Errorf("goroutineleak profile unavailable: requires Go 1.27+ runtime")
	}
	var buf bytes.Buffer
	if err := p.WriteTo(&buf, 1); err != nil {
		return 0, "", fmt.Errorf("write goroutineleak profile: %w", err)
	}
	out := buf.String()
	// Legacy count-profile format: "goroutineleak profile: total N".
	const marker = "goroutineleak profile: total "
	i := strings.Index(out, marker)
	if i < 0 {
		return 0, "", fmt.Errorf("unexpected profile output: %.200q", out)
	}
	rest := out[i+len(marker):]
	if end := strings.IndexAny(rest, "\r\n"); end >= 0 {
		rest = rest[:end]
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest))
	if err != nil {
		return 0, "", fmt.Errorf("parse profile total: %w", err)
	}
	return n, out, nil
}
