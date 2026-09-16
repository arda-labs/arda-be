package handler

import (
	"net/http/httptest"
	"testing"
)

func TestEnforceMakerCheckerFailsClosedWithoutRegistry(t *testing.T) {
	h := &WorkflowHandler{}
	r := httptest.NewRequest("POST", "/api/workflow/tasks/1/complete", nil)
	if err := h.enforceMakerChecker(r, 123, "UT_CheckerReview", "user-1"); err == nil {
		t.Fatal("checker step without an accessible registry must fail closed")
	}
}

func TestEnforceMakerCheckerSkipsMakerSteps(t *testing.T) {
	h := &WorkflowHandler{}
	r := httptest.NewRequest("POST", "/api/workflow/tasks/1/complete", nil)
	if err := h.enforceMakerChecker(r, 123, "UT_MakerInput", "user-1"); err != nil {
		t.Fatalf("maker step must not be treated as checker: %v", err)
	}
	if err := h.enforceMakerChecker(r, 123, "UT_MakerRevise", "user-1"); err != nil {
		t.Fatalf("maker revise must not be treated as checker: %v", err)
	}
}

func TestCheckerTaskStepsCoverV2ReviewElements(t *testing.T) {
	for _, id := range []string{"UT_CheckerReview", "UT_GDReview", "UT_PGDReview", "UT_BoardReview", "UT_TWRevalidate"} {
		if _, ok := checkerTaskSteps[id]; !ok {
			t.Fatalf("%s must be a checker step", id)
		}
	}
	for _, id := range []string{"UT_MakerInput", "UT_MakerRevise"} {
		if _, ok := checkerTaskSteps[id]; ok {
			t.Fatalf("%s must not be a checker step", id)
		}
	}
}

func TestNormalizeUserTaskElementID(t *testing.T) {
	cases := map[string]string{
		"Activity_CheckerReview": "UT_CheckerReview",
		"Activity_MakerRevise":   "UT_MakerRevise",
		" UT_GDReview ":          "UT_GDReview",
		"":                       "",
	}
	for input, want := range cases {
		if got := normalizeUserTaskElementID(input); got != want {
			t.Fatalf("normalizeUserTaskElementID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAuthorizeUserTaskCompleteRequiresRegistry(t *testing.T) {
	h := &WorkflowHandler{}
	r := httptest.NewRequest("POST", "/api/workflow/tasks/1/complete", nil)
	if _, err := h.authorizeUserTaskComplete(r, 1, 1, "UT_CheckerReview", "user-1"); err == nil {
		t.Fatal("complete without an accessible registry must be rejected")
	}
}
