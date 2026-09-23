package evaluation

import (
	"os"
	"testing"
)

// TestDevCorpusEvaluationSetParses keeps the committed dev golden set valid
// and well-formed. It never calls a live service, so it runs in CI.
func TestDevCorpusEvaluationSetParses(t *testing.T) {
	raw, err := os.ReadFile("../../../../scripts/ai-dev-corpus/evaluation-set.yaml")
	if err != nil {
		t.Skipf("dev corpus not present: %v", err)
	}
	set, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse dev evaluation set: %v", err)
	}
	if len(set.Cases) < 10 {
		t.Fatalf("expected at least 10 dev cases, got %d", len(set.Cases))
	}
	seen := make(map[string]struct{}, len(set.Cases))
	noAnswer := 0
	for _, c := range set.Cases {
		if c.ID == "" || c.Tenant == "" || c.Query == "" {
			t.Errorf("case must have id, tenant and query: %+v", c)
		}
		if _, ok := seen[c.ID]; ok {
			t.Errorf("duplicate case id %q", c.ID)
		}
		seen[c.ID] = struct{}{}
		if c.Expected.AllowNoAnswer {
			noAnswer++
		} else if len(c.Expected.SourceKeys) == 0 {
			t.Errorf("case %q expects an answer but lists no source_keys", c.ID)
		}
	}
	if noAnswer == 0 {
		t.Error("dev set must include at least one no-answer case")
	}
}

// TestAnswerEvaluationSetParses keeps the committed answer-level golden set
// valid offline: unique ids, at least one no-answer case, and answered cases
// must assert either citations or keywords (otherwise they cannot fail).
func TestAnswerEvaluationSetParses(t *testing.T) {
	raw, err := os.ReadFile("../../../../docs/ai/answer-evaluation-set.yaml")
	if err != nil {
		t.Skipf("answer evaluation set not present: %v", err)
	}
	set, err := ParseAnswerSet(raw)
	if err != nil {
		t.Fatalf("parse answer evaluation set: %v", err)
	}
	seen := make(map[string]struct{}, len(set.Cases))
	noAnswer := 0
	for _, c := range set.Cases {
		if _, ok := seen[c.ID]; ok {
			t.Errorf("duplicate case id %q", c.ID)
		}
		seen[c.ID] = struct{}{}
		if c.Expected.AllowNoAnswer {
			noAnswer++
			continue
		}
		if !c.Expected.MustCite && len(c.Expected.SourceKeys) == 0 && len(c.Expected.Keywords) == 0 {
			t.Errorf("case %q cannot fail: it asserts no citation, source key or keyword", c.ID)
		}
	}
	if noAnswer == 0 {
		t.Error("answer set must include at least one no-answer case")
	}
}

// TestDevCorpusAnswerSetParses keeps the dev corpus answer set valid offline.
func TestDevCorpusAnswerSetParses(t *testing.T) {
	raw, err := os.ReadFile("../../../../scripts/ai-dev-corpus/answer-evaluation-set.yaml")
	if err != nil {
		t.Skipf("dev answer set not present: %v", err)
	}
	set, err := ParseAnswerSet(raw)
	if err != nil {
		t.Fatalf("parse dev answer set: %v", err)
	}
	if len(set.Cases) < 10 {
		t.Fatalf("expected at least 10 dev answer cases, got %d", len(set.Cases))
	}
	noAnswer := 0
	for _, c := range set.Cases {
		if c.Expected.AllowNoAnswer {
			noAnswer++
		}
	}
	if noAnswer == 0 {
		t.Error("dev answer set must include at least one no-answer case")
	}
}

// TestJudgeCalibrationLabelsMatchAnswerSets keeps human labels valid and
// traceable: at least 20 labelled cases, every id present in one of the answer
// sets, and abstention labels only on allow_no_answer cases.
func TestJudgeCalibrationLabelsMatchAnswerSets(t *testing.T) {
	raw, err := os.ReadFile("../../../../scripts/ai-dev-corpus/judge-calibration.yaml")
	if err != nil {
		t.Skipf("judge calibration set not present: %v", err)
	}
	labels, err := ParseCalibrationSet(raw)
	if err != nil {
		t.Fatalf("parse calibration set: %v", err)
	}
	if len(labels.Cases) < 20 {
		t.Fatalf("expected at least 20 labelled cases, got %d", len(labels.Cases))
	}
	answerSets := []string{
		"../../../../scripts/ai-dev-corpus/answer-evaluation-set.yaml",
		"../../../../docs/ai/answer-evaluation-set.yaml",
	}
	known := map[string]AnswerCase{}
	for _, path := range answerSets {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		set, err := ParseAnswerSet(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, c := range set.Cases {
			known[c.ID] = c
		}
	}
	for _, label := range labels.Cases {
		answer, ok := known[label.ID]
		if !ok {
			t.Errorf("label %q does not match any answer set case", label.ID)
			continue
		}
		if label.CorrectAbstention != nil && !answer.Expected.AllowNoAnswer {
			t.Errorf("label %q sets correct_abstention on an answered case", label.ID)
		}
	}
}
