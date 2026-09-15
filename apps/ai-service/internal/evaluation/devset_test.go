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
