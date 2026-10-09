package events

import (
	"strings"
	"testing"
)

func TestResolveLocaleUsesPreferenceThenEnvelopeThenDefault(t *testing.T) {
	for _, tc := range []struct{ pref, envelope, fallback, want string }{
		{"vi-VN", "en-US", "en-US", "vi-VN"}, {"", "en-US", "vi-VN", "en-US"}, {"", "", "vi-VN", "vi-VN"},
	} {
		got, err := ResolveLocale(tc.pref, tc.envelope, tc.fallback)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("locale = %q, want %q", got, tc.want)
		}
	}
}

func TestResolveLocaleMissingReturnsCodedError(t *testing.T) {
	_, err := ResolveLocale("", "", "")
	if err == nil || !strings.Contains(err.Error(), "notification.locale_unavailable") {
		t.Fatalf("error = %v, want coded locale error", err)
	}
}
