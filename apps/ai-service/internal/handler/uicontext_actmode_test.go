package handler

import (
	"encoding/json"
	"testing"
)

func TestActModeFromForwardedProps(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"act", `{"ardaMode":"act"}`, true},
		{"case-insensitive", `{"ardaMode":"ACT"}`, true},
		{"ask", `{"ardaMode":"ask"}`, false},
		{"absent", `{"ardaContext":{"screen":"/loans"}}`, false},
		{"empty", ``, false},
		{"bad json", `{`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := actModeFromForwardedProps(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
