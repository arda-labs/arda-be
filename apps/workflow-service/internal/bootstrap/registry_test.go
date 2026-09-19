package bootstrap

import (
	"testing"
)

func TestDeriveRegistryStepsDPMAdditional(t *testing.T) {
	steps, err := DeriveRegistrySteps("dpm-additional-v1", dpmAdditional)
	if err != nil {
		t.Fatalf("derive steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 human steps, got %d", len(steps))
	}
	maker, checker := steps[0], steps[1]
	if maker.ElementID != "UT_MakerInput" || maker.Kind != StepKindInput {
		t.Fatalf("unexpected maker step: %+v", maker)
	}
	if maker.StepCode != "maker_input" {
		t.Fatalf("maker step code = %q, want header value maker_input", maker.StepCode)
	}
	if len(maker.AllowedActions) != 1 || maker.AllowedActions[0] != ActionSubmit {
		t.Fatalf("maker actions = %v, want [SUBMIT]", maker.AllowedActions)
	}
	if checker.ElementID != "UT_CheckerReview" || checker.Kind != StepKindChecker {
		t.Fatalf("unexpected checker step: %+v", checker)
	}
	if !contains(checker.AllowedActions, ActionApprove) ||
		!contains(checker.AllowedActions, ActionRequestChanges) ||
		!contains(checker.AllowedActions, ActionReject) {
		t.Fatalf("checker actions = %v, want approve/request-changes/reject", checker.AllowedActions)
	}
}

func TestLoanFormationReviewStepsHaveNoRejectBranch(t *testing.T) {
	steps, err := DeriveRegistrySteps("lnm-loan-formation-v2", lnmLoanFormationV2)
	if err != nil {
		t.Fatalf("derive steps: %v", err)
	}
	byElement := map[string][]string{}
	for _, step := range steps {
		byElement[step.ElementID] = step.AllowedActions
	}
	for _, element := range []string{"UT_TWRevalidate", "UT_PGDReview"} {
		if contains(byElement[element], ActionReject) {
			t.Fatalf("%s must not expose REJECT (BPMN has no reject branch): %v", element, byElement[element])
		}
	}
	for _, element := range []string{"UT_GDReview", "UT_BoardReview"} {
		if !contains(byElement[element], ActionReject) {
			t.Fatalf("%s must expose REJECT: %v", element, byElement[element])
		}
	}
}

func TestEveryBuiltInProcessDerivesRegistrySteps(t *testing.T) {
	for _, process := range BuiltInProcesses() {
		processID, err := ProcessIDOf(process.Content)
		if err != nil {
			t.Fatalf("%s: %v", process.ResourceName, err)
		}
		steps, err := DeriveRegistrySteps(processID, process.Content)
		if err != nil {
			t.Fatalf("%s: %v", process.ResourceName, err)
		}
		if len(steps) < 2 {
			t.Fatalf("%s (%s): expected at least a maker and a checker step, got %d",
				process.ResourceName, processID, len(steps))
		}
		for _, step := range steps {
			if step.ElementID == "" || step.StepCode == "" {
				t.Fatalf("%s: step with empty element/step code: %+v", process.ResourceName, step)
			}
			if step.Kind == StepKindChecker && len(step.AllowedActions) == 0 {
				t.Fatalf("%s: checker step %s has no allowed actions", process.ResourceName, step.ElementID)
			}
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
