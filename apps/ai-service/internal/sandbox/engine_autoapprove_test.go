package sandbox

import (
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func TestAutoApproveHonoursCeilingAndNeverHigh(t *testing.T) {
	cases := []struct {
		name    string
		ceiling string
		risk    string
		want    bool
	}{
		{"disabled never auto", "", "low", false},
		{"low ceiling allows low", "low", "low", true},
		{"low ceiling blocks medium", "low", "medium", false},
		{"medium ceiling allows low", "medium", "low", true},
		{"medium ceiling allows medium", "medium", "medium", true},
		{"medium ceiling blocks high", "medium", "high", false},
		{"high ceiling is not accepted", "high", "high", false},
		{"unknown risk is blocked", "medium", "critical", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := tools.Context{AutoApproveRisk: tc.ceiling}
			if got := autoApprove(scope, tc.risk); got != tc.want {
				t.Fatalf("autoApprove(%q, %q) = %v, want %v", tc.ceiling, tc.risk, got, tc.want)
			}
		})
	}
}
