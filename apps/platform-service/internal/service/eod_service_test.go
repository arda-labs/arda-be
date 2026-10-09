package service

import "testing"

func TestOrderEODStepsUsesDependenciesBeforeReporting(t *testing.T) {
	steps := []EODStepDefinition{
		{Code: "FIN_TRIAL_BALANCE_DAILY", Order: 30, DependsOn: []string{"LNM_PROVISION_DAILY", "DPM_ACCRUAL_DAILY"}},
		{Code: "LNM_PROVISION_DAILY", Order: 20, DependsOn: []string{"LNM_ACCRUAL_DAILY"}},
		{Code: "DPM_ACCRUAL_DAILY", Order: 15},
		{Code: "LNM_ACCRUAL_DAILY", Order: 10},
	}
	ordered, err := orderEODSteps(steps)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"LNM_ACCRUAL_DAILY",
		"DPM_ACCRUAL_DAILY",
		"LNM_PROVISION_DAILY",
		"FIN_TRIAL_BALANCE_DAILY",
	}
	for i, code := range want {
		if ordered[i].Code != code {
			t.Fatalf("position %d = %s, want %s (got order %v)", i, ordered[i].Code, code, stepCodes(ordered))
		}
	}
}

func TestOrderEODStepsUsesCodeAsStableTieBreak(t *testing.T) {
	steps := []EODStepDefinition{
		{Code: "ZETA", Order: 10},
		{Code: "ALPHA", Order: 10},
	}
	ordered, err := orderEODSteps(steps)
	if err != nil {
		t.Fatal(err)
	}

	if ordered[0].Code != "ALPHA" || ordered[1].Code != "ZETA" {
		t.Fatalf("equal order must tie-break by code, got %v", stepCodes(ordered))
	}
}

func stepCodes(steps []EODStepDefinition) []string {
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		out = append(out, step.Code)
	}
	return out
}
