package model

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("3"); got != 3*time.Second {
		t.Fatalf("seconds form = %v, want 3s", got)
	}
	if got := parseRetryAfter("0"); got != 0 {
		t.Fatalf("zero = %v, want 0", got)
	}
	if got := parseRetryAfter("not-a-date"); got != 0 {
		t.Fatalf("garbage = %v, want 0", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Fatalf("empty = %v, want 0", got)
	}
	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > 3*time.Second {
		t.Fatalf("http-date form = %v, want (0,3s]", got)
	}
	past := time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Fatalf("past date = %v, want 0", got)
	}
}
