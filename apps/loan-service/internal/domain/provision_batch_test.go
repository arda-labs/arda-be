package domain

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestDebtGroupProvisionRate(t *testing.T) {
	tests := []struct {
		code string
		want string
		ok   bool
	}{
		{"GROUP_1", "0", true},
		{"GROUP_2", "5", true},
		{"GROUP_3", "20", true},
		{"GROUP_4", "50", true},
		{"GROUP_5", "100", true},
		{"GROUP_UNKNOWN", "", false},
	}
	for _, tt := range tests {
		got, ok := DebtGroupProvisionRate(tt.code)
		if ok != tt.ok || (ok && !got.Equal(decimal.RequireFromString(tt.want))) {
			t.Errorf("DebtGroupProvisionRate(%q) = (%s, %v), want (%s, %v)", tt.code, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRequiredProvisionMinor(t *testing.T) {
	tests := []struct {
		name        string
		outstanding int64
		rate        string
		currency    string
		want        int64
	}{
		{"five percent", 1_000_000, "5", "VND", 50_000},
		{"half up", 1, "50", "VND", 1},
		{"zero rate", 1_000_000, "0", "VND", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RequiredProvisionMinor(tt.outstanding, decimal.RequireFromString(tt.rate), tt.currency)
			if err != nil || got != tt.want {
				t.Fatalf("RequiredProvisionMinor() = (%d, %v), want (%d, nil)", got, err, tt.want)
			}
		})
	}
}

func TestProvisionDelta(t *testing.T) {
	for _, tt := range []struct{ required, accumulated, want int64 }{{100, 40, 60}, {40, 100, -60}, {50, 50, 0}} {
		if got := ProvisionDelta(tt.required, tt.accumulated); got != tt.want {
			t.Errorf("ProvisionDelta(%d, %d) = %d, want %d", tt.required, tt.accumulated, got, tt.want)
		}
	}
}
