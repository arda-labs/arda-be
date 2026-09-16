package repository

import "testing"

func TestDefaults(t *testing.T) {
	if got := active(""); got != "ACTIVE" {
		t.Fatalf("active(\"\") = %q, want ACTIVE", got)
	}
	if got := active(" inactive "); got != "INACTIVE" {
		t.Fatalf("active(\" inactive \") = %q, want INACTIVE", got)
	}
	if got := active("SUBMITTED"); got != "SUBMITTED" {
		t.Fatalf("active(\"SUBMITTED\") = %q, want SUBMITTED", got)
	}
	if got := newID("pos"); len(got) <= len("pos_") || got[:4] != "pos_" {
		t.Fatalf("newID prefix = %q, want pos_", got)
	}
}

func TestNormalizeStatusFilter(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "legacy lower case", in: "active", want: "ACTIVE"},
		{name: "list with spaces", in: " active, inactive ", want: "ACTIVE,INACTIVE"},
		{name: "already upper", in: "SUBMITTED", want: "SUBMITTED"},
		{name: "blank entries dropped", in: ",active,,", want: "ACTIVE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeStatusFilter(tc.in); got != tc.want {
				t.Fatalf("normalizeStatusFilter(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
