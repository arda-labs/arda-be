package evaluation

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"gopkg.in/yaml.v3"
)

// AnswerSet is the answer-level evaluation set: same golden questions as the
// retrieval set, but scored on the assistant's final answer and citations
// produced by the full agent loop (model + tools + RAG).
type AnswerSet struct {
	Version     int          `yaml:"version"`
	Description string       `yaml:"description"`
	Cases       []AnswerCase `yaml:"cases"`
}

type AnswerCase struct {
	ID       string         `yaml:"id"`
	Tenant   string         `yaml:"tenant"`
	Query    string         `yaml:"query"`
	Expected AnswerExpected `yaml:"expected"`
}

type AnswerExpected struct {
	// MustCite requires at least one citation label in the answer/tool output.
	MustCite bool `yaml:"must_cite"`
	// SourceKeys are optional expected source identities (numeric source ids or
	// source paths) that must appear among the collected citations.
	SourceKeys []string `yaml:"source_keys"`
	// Keywords must all appear (case-insensitive, Unicode-normalized) in the
	// final answer text.
	Keywords []string `yaml:"keywords"`
	// AllowNoAnswer passes when the agent produced no citation at all.
	AllowNoAnswer bool `yaml:"allow_no_answer"`
}

func ParseAnswerSet(data []byte) (AnswerSet, error) {
	var set AnswerSet
	if err := yaml.Unmarshal(data, &set); err != nil {
		return AnswerSet{}, fmt.Errorf("decode answer evaluation set: %w", err)
	}
	if len(set.Cases) == 0 {
		return AnswerSet{}, fmt.Errorf("answer evaluation set has no cases")
	}
	for _, c := range set.Cases {
		if strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Query) == "" {
			return AnswerSet{}, fmt.Errorf("answer evaluation case needs id and query")
		}
	}
	return set, nil
}

// AnswerResponse is the observable result of one agent run.
type AnswerResponse struct {
	Text         string
	Citations    []string
	ToolContents []string
	LatencyMs    int
}

type AnswerFunc func(context.Context, AnswerCase) (AnswerResponse, error)

type AnswerCaseResult struct {
	ID             string   `json:"id"`
	Passed         bool     `json:"passed"`
	CitationCount  int      `json:"citation_count"`
	MissingKeys    []string `json:"missing_keys,omitempty"`
	MissingWords   []string `json:"missing_words,omitempty"`
	Error          string   `json:"error,omitempty"`
	LatencyMs      int      `json:"latency_ms"`
	AnswerExcerpt  string   `json:"answer_excerpt,omitempty"`
	CitationSample []string `json:"citation_sample,omitempty"`
}

type AnswerReport struct {
	Version    int                `json:"version"`
	Cases      []AnswerCaseResult `json:"cases"`
	Passed     int                `json:"passed"`
	Failed     int                `json:"failed"`
	PassRate   float64            `json:"pass_rate"`
	AnswerRate float64            `json:"answer_rate"`
	DurationMs int64              `json:"duration_ms"`
}

// Citation labels look like [123:heading] (source id + heading) and are
// emitted by the knowledge tool. Collect them from the answer and every tool
// result so citation checks do not depend on the model restating the source.
var citationPattern = regexp.MustCompile(`\[(\d+):[^\]]*\]`)

func RunAnswers(ctx context.Context, set AnswerSet, ask AnswerFunc) AnswerReport {
	started := time.Now()
	report := AnswerReport{Version: set.Version, Cases: make([]AnswerCaseResult, 0, len(set.Cases))}
	answered := 0
	for _, c := range set.Cases {
		result := AnswerCaseResult{ID: c.ID}
		res, err := ask(ctx, c)
		if err != nil {
			result.Error = err.Error()
			report.Failed++
			report.Cases = append(report.Cases, result)
			continue
		}
		result.LatencyMs = res.LatencyMs
		result.CitationCount = len(res.Citations)
		result.CitationSample = firstN(res.Citations, 3)
		result.AnswerExcerpt = excerpt(res.Text, 240)
		if len(res.Citations) > 0 {
			answered++
		}

		switch {
		case c.Expected.AllowNoAnswer:
			result.Passed = len(res.Citations) == 0
		default:
			passed := true
			if c.Expected.MustCite && len(res.Citations) == 0 {
				passed = false
			}
			for _, key := range c.Expected.SourceKeys {
				if !containsFold(res.Citations, key) {
					result.MissingKeys = append(result.MissingKeys, key)
					passed = false
				}
			}
			for _, word := range c.Expected.Keywords {
				if !strings.Contains(foldText(res.Text), foldText(word)) {
					result.MissingWords = append(result.MissingWords, word)
					passed = false
				}
			}
			result.Passed = passed
		}
		if result.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
		report.Cases = append(report.Cases, result)
	}
	if len(set.Cases) > 0 {
		report.PassRate = float64(report.Passed) / float64(len(set.Cases))
		report.AnswerRate = float64(answered) / float64(len(set.Cases))
	}
	report.DurationMs = time.Since(started).Milliseconds()
	return report
}

// HTTPAsk runs the full agent (POST /api/ai/agent) and parses the AG-UI SSE
// stream into answer text + citations. Identity mirrors the RAG eval runner:
// gateway headers plus an optional workload signature in production.
func HTTPAsk(baseURL, userID, tenantID, permissions, cookie string, client *http.Client) AnswerFunc {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	baseURL = strings.TrimRight(baseURL, "/")
	serviceSecret := strings.TrimSpace(os.Getenv("AI_EVAL_SERVICE_SECRET"))
	return func(ctx context.Context, c AnswerCase) (AnswerResponse, error) {
		runID := fmt.Sprintf("answer-eval-%s-%d", c.ID, time.Now().UnixNano())
		payload := map[string]any{
			"threadId": runID,
			"runId":    runID,
			"messages": []map[string]string{{"role": "user", "content": c.Query}},
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return AnswerResponse{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/ai/agent", strings.NewReader(string(body)))
		if err != nil {
			return AnswerResponse{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("X-Auth-Checked", "true")
		req.Header.Set("X-User-Id", userID)
		caseTenant := c.Tenant
		if caseTenant == "" {
			caseTenant = tenantID
		}
		req.Header.Set("X-Tenant-Id", caseTenant)
		req.Header.Set("X-Permissions", permissions)
		cookieHeader := strings.TrimSpace(cookie)
		if cookieHeader == "" {
			cookieHeader = strings.TrimSpace(os.Getenv("AI_EVAL_COOKIE"))
		}
		if cookieHeader != "" {
			req.Header.Set("Cookie", cookieHeader)
		}
		if serviceSecret != "" {
			if err := identity.SignRequest(req, serviceSecret, "auth-gateway", "ai-service", time.Now(), 2*time.Minute); err != nil {
				return AnswerResponse{}, err
			}
		}

		started := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return AnswerResponse{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			return AnswerResponse{}, fmt.Errorf("agent returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}

		response := AnswerResponse{}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event struct {
				Type    string          `json:"type"`
				Delta   string          `json:"delta"`
				Content string          `json:"content"`
				Result  json.RawMessage `json:"result"`
				Error   string          `json:"error"`
				Code    string          `json:"code"`
				Outcome json.RawMessage `json:"outcome"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				continue
			}
			switch event.Type {
			case "TEXT_MESSAGE_CONTENT":
				response.Text += event.Delta
			case "TOOL_CALL_RESULT":
				content := event.Content
				if content == "" && len(event.Result) > 0 {
					content = string(event.Result)
				}
				if content != "" {
					response.ToolContents = append(response.ToolContents, content)
				}
			case "RUN_ERROR":
				response.LatencyMs = int(time.Since(started).Milliseconds())
				return response, fmt.Errorf("run error: %s", firstNonEmpty(event.Code, event.Error))
			case "RUN_FINISHED":
				if outcomeType(event.Outcome) == "interrupt" {
					response.LatencyMs = int(time.Since(started).Milliseconds())
					return response, fmt.Errorf("run paused for human approval (%s)", c.ID)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			return response, err
		}
		response.LatencyMs = int(time.Since(started).Milliseconds())
		response.Citations = collectCitations(append(append([]string{}, response.ToolContents...), response.Text)...)
		return response, nil
	}
}

func collectCitations(sources ...string) []string {
	seen := map[string]bool{}
	var labels []string
	for _, source := range sources {
		for _, match := range citationPattern.FindAllString(source, -1) {
			if !seen[match] {
				seen[match] = true
				labels = append(labels, match)
			}
		}
	}
	return labels
}

func outcomeType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var outcome struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &outcome) != nil {
		return ""
	}
	return outcome.Type
}

func foldText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func containsFold(values []string, wanted string) bool {
	wanted = foldText(wanted)
	for _, value := range values {
		if strings.Contains(foldText(value), wanted) {
			return true
		}
	}
	return false
}

func firstN(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return values[:n]
}

func excerpt(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max] + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
