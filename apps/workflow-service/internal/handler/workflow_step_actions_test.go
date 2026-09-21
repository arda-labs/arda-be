package handler

import (
	"testing"

	"github.com/arda-labs/arda/apps/workflow-service/internal/repository"
)

func TestStepActionError(t *testing.T) {
	checker := repository.CaseTypeStep{
		ElementID:         "UT_CheckerReview",
		StepCode:          "checker_review",
		AllowedActions:    []string{"APPROVE", "REQUEST_CHANGES", "REJECT"},
		RequiredCommentOn: []string{"REQUEST_CHANGES", "REJECT"},
	}
	maker := repository.CaseTypeStep{
		ElementID:      "UT_MakerRevise",
		StepCode:       "maker_revise",
		AllowedActions: []string{"SUBMIT"},
	}
	pgd := repository.CaseTypeStep{
		ElementID:         "UT_PGDReview",
		StepCode:          "pgd_review",
		AllowedActions:    []string{"APPROVE", "REQUEST_CHANGES"},
		RequiredCommentOn: []string{"REQUEST_CHANGES", "REJECT"},
	}

	cases := []struct {
		name    string
		step    repository.CaseTypeStep
		decide  string
		comment string
		wantErr bool
	}{
		{"checker approve", checker, "APPROVE", "", false},
		{"checker request changes with comment", checker, "REQUEST_CHANGES", "bổ sung", false},
		{"checker request changes without comment", checker, "REQUEST_CHANGES", "", true},
		{"checker reject with comment", checker, "REJECT", "sai", false},
		{"checker lowercase is normalized", checker, "approve", "", false},
		{"checker invalid action", checker, "SUBMIT", "", true},
		{"maker empty decision becomes submit", maker, "", "", false},
		{"maker explicit submit", maker, "SUBMIT", "", false},
		{"maker cannot reject", maker, "REJECT", "x", true},
		{"pgd review cannot reject", pgd, "REJECT", "x", true},
		{"pgd approve", pgd, "APPROVE", "", false},
	}
	for _, tc := range cases {
		err := stepActionError(tc.step, tc.decide, tc.comment)
		if tc.wantErr != (err != nil) {
			t.Fatalf("%s: err=%v wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}

func TestContainsFold(t *testing.T) {
	if !containsFold([]string{"APPROVE", "REJECT"}, "approve") {
		t.Fatal("expected case-insensitive match")
	}
	if containsFold(nil, "APPROVE") {
		t.Fatal("nil slice must not match")
	}
	if containsFold([]string{"APPROVE"}, "SUBMIT") {
		t.Fatal("unexpected match")
	}
}
