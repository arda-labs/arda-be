package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
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
		wantFormKey := "lnm_recovery_v2." + map[string]string{
			"UT_MakerInput":    "maker_input",
			"UT_CheckerReview": "checker_review",
		}[step.ElementID]
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

func TestFormKeyNormalizesNativeStandardSteps(t *testing.T) {
	tests := []struct {
		caseType, stepCode, want string
	}{
		{"DPM_PRODUCT_REGISTER_V1", "UT_MakerInput", "dpm_product_register_v1.maker_input"},
		{"DPM_PRODUCT_REGISTER_V1", "UT_CheckerReview", "dpm_product_register_v1.checker_review"},
		{"HRM_EMPLOYEE_REGISTRATION", "UT_MakerRevise", "hrm_employee_registration.maker_revise"},
		{" dpm_product_register_v1 ", " ut_makerinput ", "dpm_product_register_v1.maker_input"},
		// Step codes that are already legacy-style stay untouched.
		{"DPM_ADDITIONAL_V1", "maker_input", "dpm_additional_v1.maker_input"},
		{"LNM_DISB_REGISTER_V2", "checker_review", "lnm_disb_register_v2.checker_review"},
		// Non-standard native steps have no FE form yet; keep lower(step code).
		{"LNM_LOAN_FORMATION_V2", "UT_GDReview", "lnm_loan_formation_v2.ut_gdreview"},
		{"LNM_LOAN_FORMATION_V2", "UT_TWRevalidate", "lnm_loan_formation_v2.ut_twrevalidate"},
	}
	for _, tt := range tests {
		if got := FormKey(tt.caseType, tt.stepCode); got != tt.want {
			t.Errorf("FormKey(%q, %q) = %q, want %q", tt.caseType, tt.stepCode, got, tt.want)
		}
	}
}

type fakeRegistryWriter struct {
	refs    []repository.OperationTypeRef
	steps   map[string][]repository.CaseTypeStep
	version map[string]int
}

func (f *fakeRegistryWriter) ListActiveOperationTypes(context.Context) ([]repository.OperationTypeRef, error) {
	return f.refs, nil
}

func (f *fakeRegistryWriter) UpsertCaseTypeSteps(_ context.Context, caseType string, version int, steps []repository.CaseTypeStep) error {
	f.steps[caseType] = steps
	f.version[caseType] = version
	return nil
}

// Case types on the shared common-maker-checker process must be seeded with
// the legacy form keys the domain remotes register, at the same registry
// version in-flight cases already pin (so the upsert rewrites rows in place).
func TestSeedRegistryUsesRemoteFormKeysForCommonMakerChecker(t *testing.T) {
	writer := &fakeRegistryWriter{
		refs: []repository.OperationTypeRef{
			{CaseType: "DPM_PRODUCT_REGISTER_V1", BpmnProcessID: "common-maker-checker", RegistryVersion: 1, Status: "ACTIVE"},
		},
		steps:   map[string][]repository.CaseTypeStep{},
		version: map[string]int{},
	}
	if err := SeedRegistry(context.Background(), writer, BuiltInProcesses()); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	if writer.version["DPM_PRODUCT_REGISTER_V1"] != 1 {
		t.Fatalf("registry version = %d, want 1 (in-flight cases pin version 1)", writer.version["DPM_PRODUCT_REGISTER_V1"])
	}
	got := map[string]string{}
	for _, step := range writer.steps["DPM_PRODUCT_REGISTER_V1"] {
		got[step.ElementID] = step.FormKey
		if step.StepCode != step.ElementID {
			t.Errorf("step code of %s = %q; native assignment/SLA config is keyed by element id", step.ElementID, step.StepCode)
		}
	}
	want := map[string]string{
		"UT_MakerInput":    "dpm_product_register_v1.maker_input",
		"UT_CheckerReview": "dpm_product_register_v1.checker_review",
	}
	for element, key := range want {
		if got[element] != key {
			t.Errorf("form key of %s = %q, want %q", element, got[element], key)
		}
	}
}
