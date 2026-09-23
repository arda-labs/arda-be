package decision

import "testing"

func choice(name string, confidence float64, probabilities map[string]float64) Choice {
	return Choice{Type: "choice", Choice: name, Confidence: &confidence, Probabilities: probabilities}
}

func TestResultSkillRequiresConfidenceForSpecializedRouting(t *testing.T) {
	result := &Result{Answers: map[string]Choice{
		"skill":        choice("report", 0.9, map[string]float64{"report": 0.95, "knowledge": 0.04, "general": 0.01}),
		"report_topic": choice("loan_portfolio", 0.85, map[string]float64{"loan_portfolio": 0.9, "other": 0.1}),
	}}
	if got := result.Skill(0.8); got != "loan_portfolio" {
		t.Fatalf("skill = %q, want loan_portfolio", got)
	}
	if got := result.Skill(0.9); got != "report" {
		t.Fatalf("skill = %q, want report when topic confidence is below threshold", got)
	}
	result.Answers["skill"] = choice("report", 0.7, map[string]float64{"report": 0.8, "knowledge": 0.1, "general": 0.1})
	if got := result.Skill(0.8); got != "general" {
		t.Fatalf("skill = %q, want general for low confidence", got)
	}
}

func TestResultSkillRejectsMalformedProbabilities(t *testing.T) {
	result := &Result{Answers: map[string]Choice{
		"skill": choice("report", 0.9, map[string]float64{"report": 1, "knowledge": 0}),
	}}
	if got := result.Skill(0.8); got != "general" {
		t.Fatalf("skill = %q, want general for malformed choice", got)
	}
}
