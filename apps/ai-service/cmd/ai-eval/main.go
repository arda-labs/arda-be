package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/arda-labs/arda/apps/ai-service/internal/evaluation"
)

// ai-eval runs the golden-set evaluations against a deployed service.
//
//	mode=retrieval (default): POST /api/rag/query per case and score recall,
//	  citations and no-answer behaviour. No tenant model needed.
//	mode=answer: run the full agent (model + tools + RAG) per case and score
//	  the final answer (citations, source keys, keywords, no-answer). Needs a
//	  configured tenant model.
//
// The answer mode used to live in a separate cmd/ai-answer-eval binary; both
// modes share the same HTTP identity options.
func main() {
	mode := flag.String("mode", envOr("AI_EVAL_MODE", "retrieval"), "evaluation mode: retrieval or answer")
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
		user := envOr("AI_EVAL_USER_ID", "ai-answer-eval")
		report := evaluation.RunAnswers(context.Background(), set,
			evaluation.HTTPAsk(base, user, os.Getenv("AI_EVAL_TENANT"), permissions, cookie, nil))
		printReport(report)
		if os.Getenv("AI_ANSWER_EVAL_STRICT") == "1" && report.Failed > 0 {
			os.Exit(2)
		}
	default:
		fatal(fmt.Errorf("unknown mode %q: expected retrieval or answer", *mode))
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

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
