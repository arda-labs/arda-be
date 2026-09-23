package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/decision"
	"github.com/arda-labs/arda/apps/ai-service/internal/evaluation"
)

// ai-eval runs the golden-set evaluations against a deployed service.
//
//	mode=retrieval (default): POST /api/rag/query per case and score recall,
//	  citations and no-answer behaviour. No tenant model needed.
//	mode=answer: run the full agent (model + tools + RAG) per case and score
//	  the final answer (citations, source keys, keywords, no-answer). Needs a
//	  configured tenant model.
//	mode=routing: call the System One decision provider directly with the
//	  production state built by decision.BuildState and score routing accuracy,
//	  macro F1, critical false positives and an offline confidence sweep. Needs
//	  a decision API key, not a tenant chat model.
//	mode=judge-calibrate: compare a judge artifact (AI_EVAL_ANSWER_ARTIFACT)
//	  against human labels (AI_EVAL_CALIBRATION_SET). No provider calls.
//
// The answer mode used to live in a separate cmd/ai-answer-eval binary; both
// modes share the same HTTP identity options.
func main() {
	mode := flag.String("mode", envOr("AI_EVAL_MODE", "retrieval"), "evaluation mode: retrieval, answer or routing")
	flag.Parse()

	base := envOr("AI_EVAL_BASE_URL", "http://localhost:8098")
	permissions := envOr("AI_EVAL_PERMISSIONS", "ai.assistant.use,ai.knowledge.read")
	cookie := os.Getenv("AI_EVAL_COOKIE")

	switch *mode {
	case "retrieval":
		path := envOr("AI_EVAL_SET", "../../docs/ai/evaluation-set.yaml")
		set, err := evaluation.Parse(readFile(path))
		if err != nil {
			fatal(err)
		}
		user := envOr("AI_EVAL_USER_ID", "ai-eval")
		report := evaluation.Run(context.Background(), set,
			evaluation.HTTPQuery(base, user, os.Getenv("AI_EVAL_TENANT"), permissions, cookie, nil))
		printReport(report)
		if os.Getenv("AI_EVAL_STRICT") == "1" && report.Failed > 0 {
			os.Exit(2)
		}
	case "answer":
		path := envOr("AI_ANSWER_EVAL_SET", "../../docs/ai/answer-evaluation-set.yaml")
		set, err := evaluation.ParseAnswerSet(readFile(path))
		if err != nil {
			fatal(err)
		}
		// ai_runs.actor_user_id is a uuid column, so the eval identity must be a
		// stable synthetic UUID rather than a label.
		user := envOr("AI_EVAL_USER_ID", "11111111-1111-4111-8111-111111111111")
		judge, judgeLabel, judgeModel := buildJudge()
		report := evaluation.RunAnswersWithJudge(context.Background(), set,
			evaluation.HTTPAsk(base, user, os.Getenv("AI_EVAL_TENANT"), permissions, cookie, nil), judge)
		printReport(report)
		if artifactPath := strings.TrimSpace(os.Getenv("AI_ANSWER_EVAL_ARTIFACT")); artifactPath != "" {
			writeArtifact(artifactPath, evaluation.AnswerArtifact{
				Judge:     judgeLabel,
				Model:     judgeModel,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Report:    report,
			})
			fmt.Printf("\nartifact written to %s\n", artifactPath)
		}
		if os.Getenv("AI_ANSWER_EVAL_STRICT") == "1" && report.Failed > 0 {
			os.Exit(2)
		}
		if judge != nil && os.Getenv("AI_EVAL_JUDGE_STRICT") == "1" {
			minGrounded := envFloat("AI_EVAL_JUDGE_MIN_GROUNDED", 0.8)
			minAnswers := envFloat("AI_EVAL_JUDGE_MIN_ANSWERS", 0.8)
			if report.GroundedRate < minGrounded || report.AnswersQuestionRate < minAnswers {
				fmt.Fprintf(os.Stderr,
					"judge gate failed: grounded %.3f (min %.3f), answers_question %.3f (min %.3f), judge_errors %d\n",
					report.GroundedRate, minGrounded, report.AnswersQuestionRate, minAnswers, report.JudgeErrors)
				os.Exit(2)
			}
		}
	case "judge-calibrate":
		runJudgeCalibration()
	case "routing":
		runRouting()
	default:
		fatal(fmt.Errorf("unknown mode %q: expected retrieval, answer or routing", *mode))
	}
}

func runRouting() {
	path := envOr("AI_EVAL_ROUTING_SET", "../../scripts/ai-dev-corpus/routing-evaluation-set.yaml")
	set, err := evaluation.ParseRoutingSet(readFile(path))
	if err != nil {
		fatal(err)
	}
	apiKey := strings.TrimSpace(os.Getenv("AI_EVAL_DECISION_API_KEY"))
	if apiKey == "" {
		fatal(errors.New("AI_EVAL_DECISION_API_KEY is required for routing mode"))
	}
	settings := decision.Settings{
		Enabled:       true,
		ModelID:       envOr("AI_EVAL_DECISION_MODEL", decision.DefaultModel),
		MinConfidence: envFloat("AI_EVAL_DECISION_MIN_CONFIDENCE", 0.8),
		APIKey:        apiKey,
	}
	if !settings.Valid() {
		fatal(fmt.Errorf("invalid decision settings: model %q min_confidence %v", settings.ModelID, settings.MinConfidence))
	}
	client := decision.NewClient(nil)
	classify := func(ctx context.Context, state string) (*decision.Result, time.Duration, error) {
		started := time.Now()
		result, err := client.Evaluate(ctx, settings, state)
		return result, time.Since(started), err
	}

	report := evaluation.RunRouting(context.Background(), set, settings.MinConfidence, classify)
	printRoutingReport(report)

	if artifactPath := strings.TrimSpace(os.Getenv("AI_EVAL_ROUTING_ARTIFACT")); artifactPath != "" {
		artifact := routingArtifact{
			Model:         settings.ModelID,
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			MinConfidence: settings.MinConfidence,
			Report:        report,
		}
		writeArtifact(artifactPath, artifact)
		fmt.Printf("\nartifact written to %s\n", artifactPath)
	}

	if os.Getenv("AI_EVAL_ROUTING_STRICT") == "1" {
		minAccuracy := envFloat("AI_EVAL_ROUTING_MIN_ACCURACY", 0.9)
		maxCritical := envFloat("AI_EVAL_ROUTING_MAX_CRITICAL_FP", 0)
		if report.Summary.Accuracy < minAccuracy || float64(report.Summary.CriticalFP) > maxCritical {
			fmt.Fprintf(os.Stderr,
				"routing gate failed: accuracy %.3f (min %.3f), critical_fp %d (max %.0f)\n",
				report.Summary.Accuracy, minAccuracy, report.Summary.CriticalFP, maxCritical)
			os.Exit(2)
		}
	}
}

type routingArtifact struct {
	Model         string                   `json:"model"`
	Timestamp     string                   `json:"timestamp"`
	MinConfidence float64                  `json:"min_confidence"`
	Report        evaluation.RoutingReport `json:"report"`
}

// buildJudge returns the optional Jev answer judge, its artifact label and
// model. Judge errors stay report-only; strict gating is a separate switch.
func buildJudge() (evaluation.JudgeFunc, string, string) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("AI_EVAL_JUDGE")), "jev") {
		return nil, "", ""
	}
	apiKey := strings.TrimSpace(os.Getenv("AI_EVAL_JUDGE_API_KEY"))
	if apiKey == "" {
		fatal(errors.New("AI_EVAL_JUDGE_API_KEY is required when AI_EVAL_JUDGE=jev"))
	}
	model := envOr("AI_EVAL_JUDGE_MODEL", decision.DefaultModel)
	settings := decision.Settings{Enabled: true, ModelID: model, MinConfidence: 0.8, APIKey: apiKey}
	if !settings.Valid() {
		fatal(fmt.Errorf("invalid judge settings: model %q", model))
	}
	return evaluation.JevJudge(decision.NewClient(nil), settings), "jev", model
}

func runJudgeCalibration() {
	labelsPath := envOr("AI_EVAL_CALIBRATION_SET", "../../scripts/ai-dev-corpus/judge-calibration.yaml")
	set, err := evaluation.ParseCalibrationSet(readFile(labelsPath))
	if err != nil {
		fatal(err)
	}
	artifactPath := strings.TrimSpace(os.Getenv("AI_EVAL_ANSWER_ARTIFACT"))
	if artifactPath == "" {
		fatal(errors.New("AI_EVAL_ANSWER_ARTIFACT is required for judge-calibrate"))
	}
	var artifact evaluation.AnswerArtifact
	if err := json.Unmarshal(readFile(artifactPath), &artifact); err != nil {
		fatal(fmt.Errorf("decode answer artifact: %w", err))
	}
	report := evaluation.RunCalibration(set, artifact)
	printCalibrationReport(report)
	if out := strings.TrimSpace(os.Getenv("AI_EVAL_CALIBRATION_ARTIFACT")); out != "" {
		writeArtifact(out, report)
		fmt.Printf("\nartifact written to %s\n", out)
	}
}

func printCalibrationReport(report evaluation.CalibrationReport) {
	fmt.Printf("judge calibration vs human labels (positive >= %.2f): missing_values=%d untracked_cases=%d judge_errors=%d\n\n",
		report.Threshold, report.MissingValues, report.UntrackedCases, report.JudgeErrors)
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "DIMENSION\tCASES\tTP\tFP\tFN\tTN\tPRECISION\tRECALL\tF1\tAGREEMENT")
	for _, row := range report.Dimensions {
		fmt.Fprintf(writer, "%s\t%d\t%d\t%d\t%d\t%d\t%.3f\t%.3f\t%.3f\t%.3f\n",
			row.Dimension, row.Cases, row.TP, row.FP, row.FN, row.TN,
			row.Precision, row.Recall, row.F1, row.Agreement)
	}
	writer.Flush()
}

func printRoutingReport(report evaluation.RoutingReport) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "CASE\tEXPECTED\tACTUAL\tCONF\tLAT(ms)\tRESULT\tNOTE")
	for _, c := range report.Cases {
		result := "pass"
		note := ""
		switch {
		case c.Error != "":
			result, note = "error", c.Error
		case c.Skipped:
			note = "skipped (oversized latest)"
		case !c.Passed:
			result = "fail"
		}
		if len(c.Violations) > 0 {
			note = "critical: routed to " + strings.Join(c.Violations, ",")
		}
		fmt.Fprintf(writer, "%s\t%s\t%s\t%.2f\t%d\t%s\t%s\n",
			c.ID, c.ExpectedSkill, c.Skill, c.Confidence, c.LatencyMs, result, note)
	}
	writer.Flush()

	summary := report.Summary
	fmt.Printf("\nsummary: cases=%d evaluated=%d skipped=%d errors=%d passed=%d failed=%d\n",
		summary.Cases, summary.Evaluated, summary.Skipped, summary.Errors, summary.Passed, summary.Failed)
	fmt.Printf("accuracy=%.3f macro_f1=%.3f critical_fp=%d p50=%dms p95=%dms tokens=%d/%d\n",
		summary.Accuracy, summary.MacroF1, summary.CriticalFP, summary.P50Ms, summary.P95Ms,
		summary.InputTokens, summary.OutputTokens)

	if len(report.PerTag) > 0 {
		fmt.Println("\nper tag:")
		for _, tag := range report.PerTag {
			fmt.Printf("  %-12s cases=%d passed=%d accuracy=%.3f\n", tag.Tag, tag.Cases, tag.Passed, tag.Accuracy)
		}
	}
	if len(report.PerSkillFP) > 0 {
		fmt.Println("\nper-skill false positives:")
		for skill, count := range report.PerSkillFP {
			fmt.Printf("  %-14s %d\n", skill, count)
		}
	}

	fmt.Println("\nthreshold sweep:")
	writer = tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "THRESHOLD\tACCURACY\tPRECISION\tRECALL\tCOVERAGE\tGENERAL\tCRITICAL_FP")
	for _, row := range report.ThresholdSweep {
		fmt.Fprintf(writer, "%.2f\t%.3f\t%.3f\t%.3f\t%.3f\t%.3f\t%d\n",
			row.Threshold, row.Accuracy, row.Precision, row.Recall, row.Coverage, row.GeneralRate, row.CriticalFP)
	}
	writer.Flush()
}

func writeArtifact(path string, value any) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		fatal(err)
	}
}

func printReport(report any) {
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(encoded))
}

func readFile(path string) []byte {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	return raw
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envFloat(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	var parsed float64
	if _, err := fmt.Sscanf(value, "%g", &parsed); err != nil {
		fatal(fmt.Errorf("%s: invalid number %q", name, value))
	}
	return parsed
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
