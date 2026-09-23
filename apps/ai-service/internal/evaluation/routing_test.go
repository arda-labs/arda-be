package evaluation

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/decision"
)

func routingTestResult(skill, topic string, skillConfidence, topicConfidence float64) *decision.Result {
	skillRest := (1 - skillConfidence) / 2
	skillProbabilities := map[string]float64{"report": skillRest, "knowledge": skillRest, "general": skillRest}
	skillProbabilities[skill] = skillConfidence
	topicProbabilities := map[string]float64{"loan_portfolio": 1 - topicConfidence, "other": 1 - topicConfidence}
	topicProbabilities[topic] = topicConfidence
	return &decision.Result{
		Model: "jev-test",
		Usage: decision.Usage{InputTokens: 10, OutputTokens: 2},
		Answers: map[string]decision.Answer{
			"skill":        decision.ChoiceAnswer(skill, skillConfidence, skillProbabilities),
			"report_topic": decision.ChoiceAnswer(topic, topicConfidence, topicProbabilities),
		},
	}
}

func TestParseRoutingSetValidatesCases(t *testing.T) {
	valid := []byte(`
version: 1
cases:
  - id: a
    messages: ["x"]
    expected_skill: report
    tags: [report]
    must_not_route: [loan_portfolio]
`)
	set, err := ParseRoutingSet(valid)
	if err != nil || len(set.Cases) != 1 {
		t.Fatalf("valid set rejected: %v %+v", err, set)
	}

	invalidSkill := []byte("version: 1\ncases:\n  - id: a\n    messages: [\"x\"]\n    expected_skill: nope\n")
	if _, err := ParseRoutingSet(invalidSkill); err == nil {
		t.Fatal("unknown expected_skill accepted")
	}

	duplicate := []byte("version: 1\ncases:\n  - id: a\n    messages: [\"x\"]\n    expected_skill: general\n  - id: a\n    messages: [\"y\"]\n    expected_skill: general\n")
	if _, err := ParseRoutingSet(duplicate); err == nil {
		t.Fatal("duplicate case id accepted")
	}

	badForbidden := []byte("version: 1\ncases:\n  - id: a\n    messages: [\"x\"]\n    expected_skill: general\n    must_not_route: [nope]\n")
	if _, err := ParseRoutingSet(badForbidden); err == nil {
		t.Fatal("unknown must_not_route skill accepted")
	}
}

func TestRunRoutingScoresConfusionAndCriticalFP(t *testing.T) {
	set := RoutingSet{Version: 1, Cases: []RoutingCase{
		{ID: "pass-report", Messages: []string{"r1"}, ExpectedSkill: "report", Tags: []string{"report"}},
		{ID: "violation-mutation", Messages: []string{"m1"}, ExpectedSkill: "general", Tags: []string{"mutation"}, MustNotRoute: []string{"report", "loan_portfolio"}},
		{ID: "provider-error", Messages: []string{"e1"}, ExpectedSkill: "general", Tags: []string{"ambiguous"}},
		{ID: "low-confidence", Messages: []string{"c1"}, ExpectedSkill: "report", Tags: []string{"report"}},
		{ID: "oversized", Messages: []string{strings.Repeat("x", 4097)}, ExpectedSkill: "general", Tags: []string{"long_input"}},
	}}
	classify := func(_ context.Context, state string) (*decision.Result, time.Duration, error) {
		switch {
		case strings.Contains(state, "r1"):
			return routingTestResult("report", "other", 0.9, 0.9), 120 * time.Millisecond, nil
		case strings.Contains(state, "m1"):
			return routingTestResult("report", "loan_portfolio", 0.9, 0.9), 100 * time.Millisecond, nil
		case strings.Contains(state, "c1"):
			return routingTestResult("report", "other", 0.7, 0.9), 80 * time.Millisecond, nil
		case strings.Contains(state, "e1"):
			return nil, 0, errors.New("provider unavailable")
		}
		return routingTestResult("general", "other", 0.9, 0.9), 50 * time.Millisecond, nil
	}

	report := RunRouting(context.Background(), set, 0.8, classify)
	summary := report.Summary
	if summary.Cases != 5 || summary.Errors != 1 || summary.Skipped != 1 || summary.Evaluated != 3 {
		t.Fatalf("unexpected counts: %+v", summary)
	}
	if summary.Passed != 2 || summary.Failed != 2 || summary.Accuracy != 0.5 {
		t.Fatalf("unexpected pass/accuracy: %+v", summary)
	}
	if summary.CriticalFP != 1 {
		t.Fatalf("critical FP = %d, want 1", summary.CriticalFP)
	}
	if summary.InputTokens != 30 || summary.OutputTokens != 6 {
		t.Fatalf("unexpected token totals: %+v", summary)
	}
	if report.Confusion["general"]["loan_portfolio"] != 1 || report.Confusion["report"]["general"] != 1 {
		t.Fatalf("unexpected confusion: %+v", report.Confusion)
	}
	if report.PerSkillFP["loan_portfolio"] != 1 || report.PerSkillFP["general"] != 1 {
		t.Fatalf("unexpected per-skill FP: %+v", report.PerSkillFP)
	}
	var mutationTag *RoutingTagResult
	for index := range report.PerTag {
		if report.PerTag[index].Tag == "mutation" {
			mutationTag = &report.PerTag[index]
		}
	}
	if mutationTag == nil || mutationTag.Cases != 1 || mutationTag.Accuracy != 0 {
		t.Fatalf("unexpected mutation tag row: %+v", report.PerTag)
	}
	if len(report.ThresholdSweep) != 10 {
		t.Fatalf("sweep rows = %d, want 10", len(report.ThresholdSweep))
	}
	if report.ThresholdSweep[0].Threshold != 0.5 || report.ThresholdSweep[0].Accuracy != 0.75 || report.ThresholdSweep[0].CriticalFP != 1 {
		t.Fatalf("unexpected 0.50 sweep row: %+v", report.ThresholdSweep[0])
	}
	if report.ThresholdSweep[6].Threshold != 0.8 || report.ThresholdSweep[6].Accuracy != 0.5 {
		t.Fatalf("unexpected 0.80 sweep row: %+v", report.ThresholdSweep[6])
	}
	sweepAtMin := -1.0
	for _, row := range report.ThresholdSweep {
		if row.Threshold == report.MinConfidence {
			sweepAtMin = row.Accuracy
		}
	}
	if sweepAtMin != report.Summary.Accuracy {
		t.Fatalf("sweep at MinConfidence = %v, headline accuracy = %v", sweepAtMin, report.Summary.Accuracy)
	}
	if report.Cases[4].Skipped != true || !report.Cases[4].Passed {
		t.Fatalf("oversized case must skip and pass as general: %+v", report.Cases[4])
	}
}

func TestRunRoutingSweepReusesOneInference(t *testing.T) {
	set := RoutingSet{Version: 1, Cases: []RoutingCase{
		{ID: "borderline", Messages: []string{"b1"}, ExpectedSkill: "report"},
	}}
	calls := 0
	classify := func(_ context.Context, _ string) (*decision.Result, time.Duration, error) {
		calls++
		return routingTestResult("report", "other", 0.7, 0.9), 10 * time.Millisecond, nil
	}
	report := RunRouting(context.Background(), set, 0.8, classify)
	if calls != 1 {
		t.Fatalf("classify called %d times, want 1", calls)
	}
	if report.Summary.Accuracy != 0 {
		t.Fatalf("headline accuracy = %v, want 0 at 0.80 threshold", report.Summary.Accuracy)
	}
	if report.ThresholdSweep[0].Accuracy != 1 {
		t.Fatalf("0.50 sweep accuracy = %v, want 1", report.ThresholdSweep[0].Accuracy)
	}
	if report.ThresholdSweep[0].Coverage != 1 || report.ThresholdSweep[0].GeneralRate != 0 {
		t.Fatalf("unexpected coverage row: %+v", report.ThresholdSweep[0])
	}
}

// TestRoutingDevSetParses keeps the committed dev routing golden set valid and
// exercises the production budgets the cases rely on. It never calls a live
// provider, so it runs in CI.
func TestRoutingDevSetParses(t *testing.T) {
	raw, err := os.ReadFile("../../../../scripts/ai-dev-corpus/routing-evaluation-set.yaml")
	if err != nil {
		t.Skipf("dev routing set not present: %v", err)
	}
	set, err := ParseRoutingSet(raw)
	if err != nil {
		t.Fatalf("parse dev routing set: %v", err)
	}
	if len(set.Cases) < 30 {
		t.Fatalf("expected at least 30 routing cases, got %d", len(set.Cases))
	}
	tags, forbidden, oversized, oversizedPrevious := map[string]int{}, 0, false, false
	for _, c := range set.Cases {
		for _, tag := range c.Tags {
			tags[tag]++
		}
		if len(c.MustNotRoute) > 0 {
			forbidden++
		}
		switch c.ID {
		case "long-input-over-4096":
			oversized = len(c.Messages[0]) > 4096
		case "followup-previous-too-long":
			oversizedPrevious = len(c.Messages[0]) > 2048
		}
	}
	for _, required := range []string{"single_turn", "followup", "mutation", "knowledge", "report", "ambiguous", "long_input"} {
		if tags[required] == 0 {
			t.Errorf("dev routing set has no %q case", required)
		}
	}
	if forbidden == 0 {
		t.Error("dev routing set has no must_not_route case")
	}
	if !oversized {
		t.Error("long-input-over-4096 latest message is not over the 4096-byte budget")
	}
	if !oversizedPrevious {
		t.Error("followup-previous-too-long previous message is not over the 2048-byte budget")
	}
}
