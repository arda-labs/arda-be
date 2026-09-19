package handler

import "testing"

func TestWithNormalizedDecision(t *testing.T) {
	variables := map[string]any{"reviewDecision": "REQUEST_CHANGES", "comment": "thiếu giấy tờ"}
	got := withNormalizedDecision(variables)
	if got["decision"] != "REQUEST_CHANGES" {
		t.Fatalf("decision = %v, want REQUEST_CHANGES", got["decision"])
	}
	explicit := withNormalizedDecision(map[string]any{"decision": "REJECT", "reviewDecision": "APPROVE"})
	if explicit["decision"] != "REJECT" {
		t.Fatalf("explicit decision must win, got %v", explicit["decision"])
	}
	if withNormalizedDecision(nil) != nil {
		t.Fatal("nil variables must stay nil")
	}
}

func TestRecordedDecision(t *testing.T) {
	if got := recordedDecision("UT_CheckerReview", map[string]any{"approvalResult": "APPROVE"}); got != "APPROVE" {
		t.Fatalf("approvalResult decision = %q, want APPROVE", got)
	}
	if got := recordedDecision("UT_MakerRevise", map[string]any{}); got != "SUBMIT" {
		t.Fatalf("maker step decision = %q, want SUBMIT", got)
	}
	if got := recordedDecision("UT_MakerInput", map[string]any{"decision": "approve"}); got != "APPROVE" {
		t.Fatalf("lower-case decision = %q, want APPROVE", got)
	}
	if got := recordedDecision("UT_TWRevalidate", map[string]any{}); got != "COMPLETE" {
		t.Fatalf("review step without decision = %q, want COMPLETE", got)
	}
}

func TestIsMakerElement(t *testing.T) {
	for _, element := range []string{"UT_MakerRevise", "UT_MakerInput", "maker_input", "Activity_MakerRevise"} {
		if !isMakerElement(element) {
			t.Fatalf("%s must be a maker element", element)
		}
	}
	if isMakerElement("UT_CheckerReview") {
		t.Fatal("UT_CheckerReview must not be a maker element")
	}
}
