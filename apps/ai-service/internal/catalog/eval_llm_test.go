package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/config"
	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// TestCatalogEvalLLM drives the production agent surface (system prompt +
// identity + full SDK typedefs + search/execute/readResult meta-tools)
// against a live OpenAI-compatible model, once per golden question, and
// records which arda.* method the model's execute() call selects, the search()
// queries it formulates on the way, and token usage per run. This is the WP7
// instrument: the "Token cơ sở / run" baseline and the tool-selection signal
// that decides when compact context or semantic re-ranking is safe.
//
// Manual and cost-incurring (one real completion per step), so it is skipped
// unless explicitly requested:
//
//	CATALOG_EVAL_LLM=1 \
//	AI_MODEL_BASE_URL=https://... AI_MODEL_API_KEY=... AI_MODEL_ID=... \
//	go test ./internal/catalog/ -run TestCatalogEvalLLM -v -timeout 20m
//
// Optional env: CATALOG_EVAL_LLM_LIMIT (run only the first N questions, for a
// smoke pass). The test never fails on misses — it is a measurement
// instrument; the CI gate remains the BM25 eval above. The full report is
// written to a JSON file whose path is logged at the end.
func TestCatalogEvalLLM(t *testing.T) {
	if os.Getenv("CATALOG_EVAL_LLM") != "1" {
		t.Skip("live-model eval: set CATALOG_EVAL_LLM=1 plus AI_MODEL_BASE_URL/AI_MODEL_API_KEY/AI_MODEL_ID to run (costs real tokens)")
	}
	cfg := config.Load()
	if cfg.ModelBaseURL == "" || cfg.ModelAPIKey == "" || cfg.ModelID == "" {
		t.Fatal("CATALOG_EVAL_LLM=1 but AI_MODEL_BASE_URL / AI_MODEL_API_KEY / AI_MODEL_ID are not all set")
	}
	provider := model.NewClient(cfg.ModelBaseURL, cfg.ModelAPIKey, cfg.ModelID, nil)
	if cfg.ModelGatewayToken != "" {
		provider = provider.WithGatewayToken(cfg.ModelGatewayToken)
	}

	questions := loadEvalQuestions(t)
	if limit := os.Getenv("CATALOG_EVAL_LLM_LIMIT"); limit != "" {
		var n int
		if _, err := fmt.Sscanf(limit, "%d", &n); err == nil && n > 0 && n < len(questions) {
			questions = questions[:n]
			t.Logf("CATALOG_EVAL_LLM_LIMIT=%d — running the first %d questions only", n, n)
		}
	}

	reg, idx := evalRegistry(t)
	typedefs := GenerateTypeDefinitions(reg.AllEntries())
	scope := evalScope()
	defs := []model.ToolDef{
		llmToolDef(tools.NewSearchMetaTool(func(query, domain string, sc tools.Context) (string, int, error) {
			entries := idx.Search(query, domain, sc, 5)
			return FormatSignatures(entries), len(entries), nil
		}).Definition()),
		// The model is only observed up to its first execute() call, so these
		// executors never run — the constructors require a function, which is
		// why the stubs exist.
		llmToolDef(tools.NewExecuteMetaTool(nil).Definition()),
		llmToolDef(tools.NewReadResultMetaTool(nil).Definition()),
	}

	var results []llmEvalResult
	for i, q := range questions {
		if i > 0 {
			// Light pacing keeps a 27-question burst under provider rate
			// limits; the retry loop below absorbs any that still land.
			time.Sleep(1500 * time.Millisecond)
		}
		res := llmEvalRun(t, provider, cfg.ModelSystemPrompt, typedefs, scope, defs, idx, q)
		results = append(results, res)
		t.Logf("[%s/%s] %s — %s (selected=%q steps=%d %s queries=%v tokens=p%d/c%d %.0fs)",
			q.Category, q.ID, verdictMark(res.Outcome), q.Expected, res.Selected, res.Steps, res.Outcome,
			res.Queries, res.PromptTokens, res.CompletionTokens, res.DurationSecs)
	}

	reportLLMEval(t, questions, results)
}

type llmEvalResult struct {
	ID               string   `json:"id"`
	Category         string   `json:"category"`
	Question         string   `json:"question"`
	Expected         string   `json:"expected"`
	Selected         string   `json:"selected,omitempty"`
	Queries          []string `json:"searchQueries,omitempty"`
	Steps            int      `json:"steps"`
	Outcome          string   `json:"outcome"` // hit | wrong-method | no-execute | text-only | error
	Error            string   `json:"error,omitempty"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	DurationSecs     float64  `json:"duration_secs"`
}

func llmEvalRun(
	t *testing.T,
	provider model.Provider,
	systemPrompt string,
	typedefs string,
	scope tools.Context,
	defs []model.ToolDef,
	idx *Index,
	q evalQuestion,
) llmEvalResult {
	t.Helper()
	res := llmEvalResult{ID: q.ID, Category: q.Category, Question: q.Question, Expected: strings.Join(q.Expected, "|")}
	started := time.Now()
	defer func() { res.DurationSecs = time.Since(started).Seconds() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	messages := []model.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "system", Content: llmIdentityContext(scope)},
		// Same injection point and wording as handler.sdkTypesMessage.
		{Role: "system", Content: "Arda SDK type definitions (source of truth for arda.* methods; search() is only needed for JSDoc detail or param confirmation):\n" + typedefs},
		{Role: "user", Content: q.Question},
	}

	const maxSteps = 4
	for step := 0; step < maxSteps && res.Outcome == ""; step++ {
		res.Steps = step + 1
		var calls []model.ToolCall
		var usage model.Usage
		// Rate-limited providers (429) are expected on a 27-question burst:
		// back off and retry instead of poisoning the run with errors.
		const maxAttempts = 4
		var err error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			_, usage, err = provider.StreamChat(ctx, messages, defs, model.StreamCallbacks{
				OnToolCall: func(c model.ToolCall) { calls = append(calls, c) },
				OnFinish:   func(_ string, u model.Usage) { usage = u },
			})
			if err == nil || !strings.Contains(err.Error(), "429") {
				break
			}
			calls = calls[:0]
			select {
			case <-ctx.Done():
			case <-time.After(time.Duration(attempt) * 15 * time.Second):
			}
		}
		if usage.PromptTokens > 0 {
			res.PromptTokens += usage.PromptTokens
			res.CompletionTokens += usage.CompletionTokens
		}
		if err != nil {
			res.Outcome, res.Error = "error", err.Error()
			return res
		}
		if len(calls) == 0 {
			res.Outcome = "text-only"
			return res
		}

		assistant := model.Message{Role: "assistant", ToolCalls: calls}
		messages = append(messages, assistant)
		for _, call := range calls {
			switch call.Name {
			case "execute":
				res.Selected = firstSDKMethod(call.Arguments)
				if slices.Contains(q.Expected, res.Selected) {
					res.Outcome = "hit"
				} else {
					res.Outcome = "wrong-method"
				}
				return res
			case "search":
				res.Queries = append(res.Queries, searchQueryOf(call.Arguments))
				query, domain := searchQueryOf(call.Arguments), searchDomainOf(call.Arguments)
				entries := idx.Search(query, domain, scope, 5)
				messages = append(messages, model.Message{
					Role: "tool", ToolCallID: call.ID,
					Content: FormatSignatures(entries),
				})
			default: // readResult or an unknown tool name — no data to feed back
				messages = append(messages, model.Message{
					Role: "tool", ToolCallID: call.ID,
					Content: `{"error":"no stored results"}`,
				})
			}
		}
	}
	if res.Outcome == "" {
		res.Outcome = "no-execute"
	}
	return res
}

// llmIdentityContext mirrors handler.buildIdentityContext (deliberately
// without the permission catalog — the model must discover capabilities).
func llmIdentityContext(scope tools.Context) string {
	var b strings.Builder
	b.WriteString("Current actor:\n")
	fmt.Fprintf(&b, "- user_id: %s\n", scope.ActorUserID)
	fmt.Fprintf(&b, "- username: %s\n", scope.Username)
	fmt.Fprintf(&b, "- tenant_id: %s\n", scope.TenantID)
	fmt.Fprintf(&b, "- org_id: %s\n", scope.ActiveOrgID)
	b.WriteString("\nAuthorization:\n")
	b.WriteString("- Use only capabilities exposed by the tool layer.\n")
	b.WriteString("- Authorization is enforced at execution time.\n")
	return b.String()
}

func llmToolDef(d tools.Definition) model.ToolDef {
	return model.ToolDef{Name: d.Name, Description: d.Description, Parameters: d.Parameters}
}

var sdkCallPattern = regexp.MustCompile(`arda\.[a-zA-Z][a-zA-Z0-9]*(?:\.[a-zA-Z][a-zA-Z0-9]*)+\s*\(`)

// firstSDKMethod extracts the first arda.* method mentioned in an execute()
// tool-call argument payload ({"code": "..."}).
func firstSDKMethod(arguments string) string {
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return ""
	}
	match := sdkCallPattern.FindString(payload.Code)
	return strings.TrimSuffix(strings.TrimPrefix(match, "arda."), "(")
}

func searchQueryOf(arguments string) string {
	var payload struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal([]byte(arguments), &payload)
	return payload.Query
}

func searchDomainOf(arguments string) string {
	var payload struct {
		Domain string `json:"domain"`
	}
	_ = json.Unmarshal([]byte(arguments), &payload)
	return payload.Domain
}

func verdictMark(outcome string) string {
	if outcome == "hit" {
		return "HIT "
	}
	return "MISS"
}

// reportLLMEval logs the per-category aggregates and writes the JSON report.
func reportLLMEval(t *testing.T, questions []evalQuestion, results []llmEvalResult) {
	t.Helper()

	type bucket struct {
		hits, wrong, other int
		promptTokens       int
	}
	buckets := map[string]*bucket{}
	for _, r := range results {
		b, ok := buckets[r.Category]
		if !ok {
			b = &bucket{}
			buckets[r.Category] = b
		}
		switch r.Outcome {
		case "hit":
			b.hits++
		case "wrong-method":
			b.wrong++
		default:
			b.other++
		}
		b.promptTokens += r.PromptTokens
	}

	t.Logf("=== LLM catalog eval: %d questions ===", len(results))
	order := []string{"keyword", "paraphrase", "boundary"}
	for _, category := range order {
		b, ok := buckets[category]
		if !ok {
			continue
		}
		t.Logf("%-11s: %d/%d hit, %d wrong-method, %d other (avg prompt tokens %d)",
			category, b.hits, countCategory(questions, category), b.wrong, b.other,
			b.promptTokens/max(1, b.hits+b.wrong+b.other))
	}

	path := filepath.Join(os.TempDir(), "catalog-eval-llm-report.json")
	raw, err := json.MarshalIndent(results, "", "  ")
	if err == nil {
		if writeErr := os.WriteFile(path, raw, 0o600); writeErr == nil {
			t.Logf("full report: %s", path)
		}
	}
}
