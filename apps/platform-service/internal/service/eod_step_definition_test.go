package service

import "testing"

func TestOrderEODStepsHonorsDependenciesAndStableOrder(t *testing.T) {
	steps := []EODStepDefinition{
		{Code: "REPORT", Order: 30, DependsOn: []string{"LOAN", "DEPOSIT"}},
		{Code: "DEPOSIT", Order: 20},
		{Code: "LOAN", Order: 10},
	}
	ordered, err := orderEODSteps(steps)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"LOAN", "DEPOSIT", "REPORT"}
	for i, step := range ordered {
		if step.Code != want[i] {
			t.Fatalf("ordered[%d] = %s, want %s", i, step.Code, want[i])
		}
	}
}

func TestOrderEODStepsRejectsMissingDependenciesAndCycles(t *testing.T) {
	if _, err := orderEODSteps([]EODStepDefinition{{Code: "A", DependsOn: []string{"MISSING"}}}); err == nil {
		t.Fatal("expected missing dependency to fail validation")
	}
	cycle := []EODStepDefinition{
		{Code: "A", DependsOn: []string{"B"}},
		{Code: "B", DependsOn: []string{"A"}},
	}
	if _, err := orderEODSteps(cycle); err == nil {
		t.Fatal("expected dependency cycle to fail validation")
	}
}

func TestOrderEODStepsRejectsDuplicateCodes(t *testing.T) {
	if _, err := orderEODSteps([]EODStepDefinition{{Code: "A"}, {Code: "A"}}); err == nil {
		t.Fatal("expected duplicate step code to fail validation")
	}
}
