package handler

import (
	"net/http"
	"testing"
)

func TestStripAuthContextHeaders(t *testing.T) {
	header := http.Header{
		"X-User-Id":      {"user-1"},
		"X-Auth-Checked": {"true"},
		"X-Auth-Time":    {"123"},
		"Authorization":  {"Bearer token"},
	}

	stripAuthContextHeaders(header)

	for _, key := range []string{"X-User-Id", "X-Auth-Checked", "X-Auth-Time"} {
		if got := header.Get(key); got != "" {
			t.Fatalf("%s was not stripped: %q", key, got)
		}
	}
	if got := header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization was changed: %q", got)
	}
}
