package evaluation

import "testing"

func boolPointer(value bool) *bool { return &value }

func floatPointer(value float64) *float64 { return &value }

func TestParseCalibrationSetValidatesCases(t *testing.T) {
	valid := []byte("version: 1\ncases:\n  - id: a\n    grounded: true\n    answers_question: false\n")
	set, err := ParseCalibrationSet(valid)
	if err != nil || len(set.Cases) != 1 || set.Cases[0].Grounded == nil || !*set.Cases[0].Grounded {
		t.Fatalf("valid set rejected: %v %+v", err, set)
	}
	if _, err := ParseCalibrationSet([]byte("version: 1\ncases:\n  - grounded: true\n")); err == nil {
		t.Fatal("case without id accepted")
	}
	duplicate := []byte("version: 1\ncases:\n  - id: a\n  - id: a\n")
	if _, err := ParseCalibrationSet(duplicate); err == nil {
		t.Fatal("duplicate id accepted")
	}
}

func TestRunCalibrationComputesPerDimensionConfusion(t *testing.T) {
	set := CalibrationSet{Version: 1, Cases: []CalibrationCase{
		{ID: "a", Grounded: boolPointer(true), AnswersQuestion: boolPointer(true)},
		{ID: "b", Grounded: boolPointer(false), AnswersQuestion: boolPointer(true)},
		{ID: "c", Grounded: boolPointer(true)},
		{ID: "missing"},
	}}
	artifact := AnswerArtifact{Report: AnswerReport{Cases: []AnswerCaseResult{
		{ID: "a", Grounded: floatPointer(0.9), AnswersQuestion: floatPointer(0.7)},
		{ID: "b", Grounded: floatPointer(0.8), AnswersQuestion: floatPointer(0.2)},
		{ID: "c", JudgeError: "provider unavailable"},
	}}}
	report := RunCalibration(set, artifact)
	if report.MissingValues != 1 || report.UntrackedCases != 1 || report.JudgeErrors != 1 {
		t.Fatalf("unexpected counters: %+v", report)
	}
	dimensions := map[string]CalibrationDimension{}
	for _, row := range report.Dimensions {
		dimensions[row.Dimension] = row
	}
	grounded := dimensions["grounded"]
	if grounded.Cases != 2 || grounded.TP != 1 || grounded.FP != 1 || grounded.Precision != 0.5 || grounded.Recall != 1 {
		t.Fatalf("unexpected grounded row: %+v", grounded)
	}
	answers := dimensions["answers_question"]
	if answers.Cases != 2 || answers.TP != 1 || answers.FN != 1 || answers.Precision != 1 || answers.Recall != 0.5 {
		t.Fatalf("unexpected answers_question row: %+v", answers)
	}
	if len(report.Dimensions) != 2 {
		t.Fatalf("dimensions = %d, want 2 (no abstention labels)", len(report.Dimensions))
	}
	if report.Threshold != 0.5 {
		t.Fatalf("threshold = %v", report.Threshold)
	}
}
