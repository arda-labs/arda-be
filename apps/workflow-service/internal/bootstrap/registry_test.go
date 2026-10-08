package bootstrap

import (
	"strings"
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

func TestCommonMakerCheckerRegistryStepsAreCaseTypeSpecific(t *testing.T) {
	var content []byte
	for _, process := range BuiltInProcesses() {
		if process.ProcessCode == "COMMON_MAKER_CHECKER" {
			content = process.Content
			break
		}
	}
	if len(content) == 0 {
		t.Fatal("common maker-checker process not registered")
	}
	steps, err := DeriveRegistrySteps("common-maker-checker", content)
	if err != nil {
		t.Fatalf("derive shared process steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("common maker-checker steps = %d, want maker + checker", len(steps))
	}
	want := []struct {
		element string
		code    string
		kind    StepKind
	}{
		{"UT_MakerInput", "UT_MakerInput", StepKindInput},
		{"UT_CheckerReview", "UT_CheckerReview", StepKindChecker},
	}
	for i, step := range steps {
		if step.ElementID != want[i].element || step.StepCode != want[i].code || step.Kind != want[i].kind {
			t.Fatalf("step %d = %+v, want %+v", i, step, want[i])
		}
		wantFormKey := "lnm_recovery_v2." + strings.ToLower(step.ElementID)
		if got := FormKey("LNM_RECOVERY_V2", step.StepCode); got != wantFormKey {
			t.Fatalf("recovery form key = %q, want %q", got, wantFormKey)
		}
		if FormKey("LNM_RECOVERY_V2", step.StepCode) == FormKey("LNM_WAIVER_V2", step.StepCode) {
			t.Fatalf("shared step form key must remain case-type specific: %s", step.StepCode)
		}
	}
}

func TestBatchDisbursementRegistryDoesNotOfferUnsupportedRequestChanges(t *testing.T) {
	for _, caseType := range []string{"LNM_DISB_BATCH_REGISTER_V2", "LNM_DISB_BATCH_COMPLETE_V2"} {
		steps, err := DeriveRegistryStepsForCaseType(caseType, "common-maker-checker", commonMakerChecker)
		if err != nil {
			t.Fatalf("derive %s steps: %v", caseType, err)
		}
		checker := steps[len(steps)-1]
		if contains(checker.AllowedActions, ActionRequestChanges) {
			t.Errorf("%s checker actions = %v; batch has no submitted-batch edit endpoint", caseType, checker.AllowedActions)
		}
		if !contains(checker.AllowedActions, ActionApprove) || !contains(checker.AllowedActions, ActionReject) {
			t.Errorf("%s checker actions = %v; want APPROVE and REJECT", caseType, checker.AllowedActions)
		}
	}
}

func TestCommonMakerCheckerCanSkipInitialMakerInput(t *testing.T) {
	if !strings.Contains(string(commonMakerChecker), `=mcSkipMakerInput = true`) {
		t.Fatal("shared process must route submitted batches to validation without opening a duplicate maker task")
	}
	if !strings.Contains(string(commonMakerChecker), `id="Flow_ValidationError_Cancel"`) {
		t.Fatal("batch validation errors must end through cancel when the submitted batch cannot be edited")
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
