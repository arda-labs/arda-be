package repository

import "testing"

func TestDefaults(t *testing.T) {
	if got := active(""); got != "active" {
		t.Fatalf("active(\"\") = %q, want active", got)
	}
	if got := active("inactive"); got != "inactive" {
		t.Fatalf("active(\"inactive\") = %q, want inactive", got)
	}
	if got := newID("pos"); len(got) <= len("pos_") || got[:4] != "pos_" {
		t.Fatalf("newID prefix = %q, want pos_", got)
	}
}
