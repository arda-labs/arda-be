package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/arda-labs/arda/apps/ai-service/internal/evaluation"
)

// ai-answer-eval runs the full agent (model + tools + RAG) over the
// answer-level golden set and scores the final answer: citations, expected
// source keys, expected keywords and no-answer behaviour. Unlike ai-eval
// (retrieval only), this needs a configured tenant model.
func main() {
	path := os.Getenv("AI_ANSWER_EVAL_SET")
	if path == "" {
		path = "../../docs/ai/answer-evaluation-set.yaml"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fatal(err)
	}
	set, err := evaluation.ParseAnswerSet(raw)
	if err != nil {
		fatal(err)
	}
	base := os.Getenv("AI_EVAL_BASE_URL")
	if base == "" {
		base = "http://localhost:8098"
	}
	user := os.Getenv("AI_EVAL_USER_ID")
	if user == "" {
		user = "ai-answer-eval"
	}
	permissions := os.Getenv("AI_EVAL_PERMISSIONS")
	if permissions == "" {
		permissions = "ai.assistant.use,ai.knowledge.read"
	}
	cookie := os.Getenv("AI_EVAL_COOKIE")
	report := evaluation.RunAnswers(
		context.Background(),
		set,
		evaluation.HTTPAsk(base, user, os.Getenv("AI_EVAL_TENANT"), permissions, cookie, nil),
	)
	encoded, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(encoded))
	if os.Getenv("AI_ANSWER_EVAL_STRICT") == "1" && report.Failed > 0 {
		os.Exit(2)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
