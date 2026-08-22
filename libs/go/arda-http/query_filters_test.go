package ardahttp

import (
	"net/url"
	"testing"
)

func TestParseOptionalBool(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *bool
		wantErr bool
	}{
		{name: "missing"},
		{name: "true", raw: "true", want: boolPointer(true)},
		{name: "false", raw: "false", want: boolPointer(false)},
		{name: "invalid", raw: "yes", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOptionalBool(url.Values{"is_active": {tt.raw}}, "is_active")
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseOptionalBool() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.want == nil && got != nil {
				t.Fatalf("ParseOptionalBool() = %v, want nil", *got)
			}
			if tt.want != nil && (got == nil || *got != *tt.want) {
				t.Fatalf("ParseOptionalBool() = %v, want %v", got, *tt.want)
			}
		})
	}
}

func TestParseCSVQuery(t *testing.T) {
	got := ParseCSVQuery(url.Values{"status": {"active, pending,active,,"}}, "status")
	want := []string{"active", "pending"}
	if len(got) != len(want) {
		t.Fatalf("ParseCSVQuery() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ParseCSVQuery() = %v, want %v", got, want)
		}
	}
}

func TestParseOptionalEnum(t *testing.T) {
	values := url.Values{"status": {"active"}}
	got, err := ParseOptionalEnum(values, "status", "active", "inactive")
	if err != nil || got != "active" {
		t.Fatalf("ParseOptionalEnum() = %q, %v", got, err)
	}

	values.Set("status", "deleted")
	if _, err := ParseOptionalEnum(values, "status", "active", "inactive"); err == nil {
		t.Fatal("ParseOptionalEnum() expected error")
	}
}

func boolPointer(value bool) *bool {
	return &value
}
