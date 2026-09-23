package evaluation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/decision"
	"gopkg.in/yaml.v3"
)

// RoutingSet is the routing golden set: user messages in, one expected skill
// out. Cases run against the System One provider directly (no ai-service hop),
// mirroring the production state built by decision.BuildState.
type RoutingSet struct {
	Version     int           `yaml:"version"`
	Description string        `yaml:"description"`
	Metrics     []string      `yaml:"metrics"`
	Cases       []RoutingCase `yaml:"cases"`
}

type RoutingCase struct {
	ID            string   `yaml:"id"`
	Messages      []string `yaml:"messages"`
	ExpectedSkill string   `yaml:"expected_skill"`
	Tags          []string `yaml:"tags"`
	MustNotRoute  []string `yaml:"must_not_route"`
}

var routingSkills = []string{"general", "report", "knowledge", "loan_portfolio"}

func isRoutingSkill(value string) bool {
	for _, skill := range routingSkills {
		if skill == value {
			return true
		}
	}
	return false
}

func ParseRoutingSet(data []byte) (RoutingSet, error) {
	var set RoutingSet
	if err := yaml.Unmarshal(data, &set); err != nil {
		return RoutingSet{}, fmt.Errorf("decode routing evaluation set: %w", err)
	}
	if len(set.Cases) == 0 {
		return RoutingSet{}, fmt.Errorf("routing evaluation set has no cases")
	}
	seen := make(map[string]struct{}, len(set.Cases))
	for _, c := range set.Cases {
		if strings.TrimSpace(c.ID) == "" || len(c.Messages) == 0 {
			return RoutingSet{}, fmt.Errorf("routing case needs id and messages")
		}
		if _, ok := seen[c.ID]; ok {
			return RoutingSet{}, fmt.Errorf("duplicate routing case id %q", c.ID)
		}
		seen[c.ID] = struct{}{}
		if !isRoutingSkill(c.ExpectedSkill) {
			return RoutingSet{}, fmt.Errorf("routing case %q has unknown expected_skill %q", c.ID, c.ExpectedSkill)
		}
		for _, skill := range c.MustNotRoute {
			if !isRoutingSkill(skill) {
				return RoutingSet{}, fmt.Errorf("routing case %q has unknown must_not_route skill %q", c.ID, skill)
			}
		}
	}
	return set, nil
}

// ClassifyFunc runs one classification. It receives the production state
// string built by decision.BuildState; the caller owns provider settings.
type ClassifyFunc func(ctx context.Context, state string) (*decision.Result, time.Duration, error)

type RoutingCaseResult struct {
	ID            string   `json:"id"`
	ExpectedSkill string   `json:"expected_skill"`
	Skill         string   `json:"skill"`
	Confidence    float64  `json:"confidence"`
	LatencyMs     int64    `json:"latency_ms"`
	Tags          []string `json:"tags,omitempty"`
	Skipped       bool     `json:"skipped,omitempty"`
	Passed        bool     `json:"passed"`
	Violations    []string `json:"violations,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type RoutingSummary struct {
	Cases        int     `json:"cases"`
	Evaluated    int     `json:"evaluated"`
	Skipped      int     `json:"skipped"`
	Errors       int     `json:"errors"`
	Passed       int     `json:"passed"`
	Failed       int     `json:"failed"`
	Accuracy     float64 `json:"accuracy"`
	MacroF1      float64 `json:"macro_f1"`
	CriticalFP   int     `json:"critical_fp"`
	P50Ms        int64   `json:"p50_ms"`
	P95Ms        int64   `json:"p95_ms"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
}

type RoutingTagResult struct {
	Tag      string  `json:"tag"`
	Cases    int     `json:"cases"`
	Passed   int     `json:"passed"`
	Accuracy float64 `json:"accuracy"`
}

type ConfidenceBucket struct {
	Range string `json:"range"`
	Count int    `json:"count"`
}

// RoutingThresholdResult is one row of the offline confidence sweep: the same
// provider answers re-thresholded with decision.Result.Skill, never re-asked.
type RoutingThresholdResult struct {
	Threshold   float64 `json:"threshold"`
	Accuracy    float64 `json:"accuracy"`
	Precision   float64 `json:"precision"`
	Recall      float64 `json:"recall"`
	Coverage    float64 `json:"coverage"`
	GeneralRate float64 `json:"general_rate"`
	CriticalFP  int     `json:"critical_fp"`
}

type RoutingReport struct {
	Version        int                      `json:"version"`
	MinConfidence  float64                  `json:"min_confidence"`
	Summary        RoutingSummary           `json:"summary"`
	PerTag         []RoutingTagResult       `json:"per_tag"`
	Confusion      map[string]map[string]int `json:"confusion"`
	PerSkillFP     map[string]int           `json:"per_skill_fp"`
	Confidence     []ConfidenceBucket       `json:"confidence"`
	Cases          []RoutingCaseResult      `json:"cases"`
	ThresholdSweep []RoutingThresholdResult `json:"threshold_sweep"`
	DurationMs     int64                    `json:"duration_ms"`
}

// routingOutcome keeps one provider answer around so the threshold sweep can
// re-evaluate it without new provider calls.
type routingOutcome struct {
	c      RoutingCase
	result *decision.Result
	skipped bool
}

func routingMessages(messages []string) []decision.Message {
	out := make([]decision.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, decision.Message{Role: "user", Content: message})
	}
	return out
}

func skillConfidence(result *decision.Result) float64 {
	return result.ChoiceConfidence("skill")
}

func RunRouting(ctx context.Context, set RoutingSet, minConfidence float64, classify ClassifyFunc) RoutingReport {
	started := time.Now()
	report := RoutingReport{
		Version:       set.Version,
		MinConfidence: minConfidence,
		Cases:         make([]RoutingCaseResult, 0, len(set.Cases)),
		Confusion:     map[string]map[string]int{},
		PerSkillFP:    map[string]int{},
	}
	report.Summary.Cases = len(set.Cases)

	outcomes := make([]routingOutcome, 0, len(set.Cases))
	var latencies []int64

	for _, c := range set.Cases {
		outcome := routingOutcome{c: c}
		result := RoutingCaseResult{ID: c.ID, ExpectedSkill: c.ExpectedSkill, Tags: c.Tags}
		state, ok := decision.BuildState(routingMessages(c.Messages))
		switch {
		case !ok:
			// Production skips classification for oversized latest messages
			// and falls back to the general path.
			result.Skipped = true
			result.Skill = "general"
			outcome.skipped = true
			report.Summary.Skipped++
		default:
			classified, latency, err := classify(ctx, state)
			if err != nil {
				result.Error = err.Error()
				report.Summary.Errors++
				report.Cases = append(report.Cases, result)
				outcomes = append(outcomes, routingOutcome{c: c})
				continue
			}
			result.LatencyMs = latency.Milliseconds()
			result.Skill = classified.Skill(minConfidence)
			result.Confidence = skillConfidence(classified)
			latencies = append(latencies, result.LatencyMs)
			report.Summary.InputTokens += classified.Usage.InputTokens
			report.Summary.OutputTokens += classified.Usage.OutputTokens
			outcome.result = classified
		}
		result.Passed = result.Skill == c.ExpectedSkill
		if !result.Skipped {
			for _, forbidden := range c.MustNotRoute {
				if result.Skill == forbidden {
					result.Violations = append(result.Violations, forbidden)
				}
			}
			if len(result.Violations) > 0 {
				report.Summary.CriticalFP++
			}
		}
		if result.Passed {
			report.Summary.Passed++
		} else {
			report.Summary.Failed++
		}
		report.Cases = append(report.Cases, result)
		outcomes = append(outcomes, outcome)
	}

	report.Summary.Evaluated = len(set.Cases) - report.Summary.Errors - report.Summary.Skipped
	if predictions := len(set.Cases) - report.Summary.Errors; predictions > 0 {
		report.Summary.Accuracy = float64(report.Summary.Passed) / float64(predictions)
	}
	report.Confusion = confusionOf(outcomes, minConfidence)
	report.Summary.MacroF1, _, _ = macroScores(outcomes, minConfidence)
	for skill, count := range falsePositives(outcomes, minConfidence) {
		report.PerSkillFP[skill] = count
	}
	report.PerTag = perTagResults(set.Cases, report.Cases)
	report.Confidence = confidenceBuckets(report.Cases)
	report.Summary.P50Ms = percentile(latencies, 0.50)
	report.Summary.P95Ms = percentile(latencies, 0.95)
	report.ThresholdSweep = thresholdSweep(outcomes)
	report.DurationMs = time.Since(started).Milliseconds()
	return report
}

// routingPrediction returns the skill the report would route to at threshold t.
// Errors and unclassified outcomes are excluded.
func routingPrediction(outcome routingOutcome, threshold float64) (string, bool) {
	if outcome.skipped {
		return "general", true
	}
	if outcome.result == nil {
		return "", false
	}
	return outcome.result.Skill(threshold), true
}

func confusionOf(outcomes []routingOutcome, threshold float64) map[string]map[string]int {
	confusion := map[string]map[string]int{}
	for _, outcome := range outcomes {
		predicted, ok := routingPrediction(outcome, threshold)
		if !ok {
			continue
		}
		if confusion[outcome.c.ExpectedSkill] == nil {
			confusion[outcome.c.ExpectedSkill] = map[string]int{}
		}
		confusion[outcome.c.ExpectedSkill][predicted]++
	}
	return confusion
}

func falsePositives(outcomes []routingOutcome, threshold float64) map[string]int {
	fp := map[string]int{}
	for _, outcome := range outcomes {
		predicted, ok := routingPrediction(outcome, threshold)
		if !ok || predicted == outcome.c.ExpectedSkill {
			continue
		}
		fp[predicted]++
	}
	return fp
}

// macroScores returns macro-F1 plus macro precision and recall over the four
// skills. Classes without support are excluded so a skill missing from a set
// does not silently lower the score.
func macroScores(outcomes []routingOutcome, threshold float64) (float64, float64, float64) {
	confusion := confusionOf(outcomes, threshold)
	tp, fp, fn := map[string]int{}, map[string]int{}, map[string]int{}
	for expected, row := range confusion {
		for actual, count := range row {
			if expected == actual {
				tp[expected] += count
			} else {
				fp[actual] += count
				fn[expected] += count
			}
		}
	}
	precisionSum, recallSum, f1Sum, classes := 0.0, 0.0, 0.0, 0
	for _, skill := range routingSkills {
		support := tp[skill] + fn[skill]
		if support == 0 {
			continue
		}
		precision := 1.0
		if tp[skill]+fp[skill] > 0 {
			precision = float64(tp[skill]) / float64(tp[skill]+fp[skill])
		}
		recall := float64(tp[skill]) / float64(support)
		f1 := 0.0
		if precision+recall > 0 {
			f1 = 2 * precision * recall / (precision + recall)
		}
		precisionSum += precision
		recallSum += recall
		f1Sum += f1
		classes++
	}
	if classes == 0 {
		return 0, 0, 0
	}
	count := float64(classes)
	return f1Sum / count, precisionSum / count, recallSum / count
}

func thresholdSweep(outcomes []routingOutcome) []RoutingThresholdResult {
	var sweep []RoutingThresholdResult
	for step := 0; step <= 9; step++ {
		// Round so the threshold matches the displayed value and the production
		// MinConfidence comparison; binary accumulation gives 0.8000000000000002.
		threshold := math.Round((0.5+float64(step)*0.05)*100) / 100
		predictions, correct, coverage, critical := 0, 0, 0, 0
		for _, outcome := range outcomes {
			predicted, ok := routingPrediction(outcome, threshold)
			if !ok {
				continue
			}
			predictions++
			if predicted == outcome.c.ExpectedSkill {
				correct++
			}
			if predicted != "general" {
				coverage++
			}
			for _, forbidden := range outcome.c.MustNotRoute {
				if predicted == forbidden {
					critical++
					break
				}
			}
		}
		row := RoutingThresholdResult{Threshold: threshold, CriticalFP: critical}
		if predictions > 0 {
			row.Accuracy = float64(correct) / float64(predictions)
			row.Coverage = float64(coverage) / float64(predictions)
			row.GeneralRate = float64(predictions-coverage) / float64(predictions)
		}
		_, row.Precision, row.Recall = macroScores(outcomes, threshold)
		sweep = append(sweep, row)
	}
	return sweep
}

func perTagResults(cases []RoutingCase, results []RoutingCaseResult) []RoutingTagResult {
	type tagCount struct{ cases, passed int }
	counts := map[string]*tagCount{}
	order := []string{}
	for index, c := range cases {
		if index >= len(results) || results[index].Error != "" {
			continue
		}
		for _, tag := range c.Tags {
			if counts[tag] == nil {
				counts[tag] = &tagCount{}
				order = append(order, tag)
			}
			counts[tag].cases++
			if results[index].Passed {
				counts[tag].passed++
			}
		}
	}
	sort.Strings(order)
	out := make([]RoutingTagResult, 0, len(order))
	for _, tag := range order {
		count := counts[tag]
		row := RoutingTagResult{Tag: tag, Cases: count.cases, Passed: count.passed}
		if count.cases > 0 {
			row.Accuracy = float64(count.passed) / float64(count.cases)
		}
		out = append(out, row)
	}
	return out
}

func confidenceBuckets(results []RoutingCaseResult) []ConfidenceBucket {
	ranges := []struct {
		label string
		min   float64
		max   float64
	}{
		{"[0.0,0.5)", 0, 0.5},
		{"[0.5,0.6)", 0.5, 0.6},
		{"[0.6,0.7)", 0.6, 0.7},
		{"[0.7,0.8)", 0.7, 0.8},
		{"[0.8,0.9)", 0.8, 0.9},
		{"[0.9,1.0]", 0.9, 1.0001},
	}
	buckets := make([]ConfidenceBucket, 0, len(ranges))
	for _, entry := range ranges {
		bucket := ConfidenceBucket{Range: entry.label}
		for _, result := range results {
			if result.Error != "" || result.Skipped {
				continue
			}
			if result.Confidence >= entry.min && result.Confidence < entry.max {
				bucket.Count++
			}
		}
		buckets = append(buckets, bucket)
	}
	return buckets
}

func percentile(values []int64, fraction float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64{}, values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil(fraction*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
