package handler

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
)

// In-process Prometheus metrics for the AI runtime, rendered into the shared
// /metrics endpoint via ardahttp.MetricsMiddleware extraRenderers. Counters
// reset on restart; the deployed Prometheus is expected to scrape frequently
// enough for the aggregate views this spike exposes.

type aiCounterVec struct {
	name  string
	help  string
	mu    sync.Mutex
	// labelValues joined by \x00 → value
	values map[string]uint64
	labels []string
}

func newAICounterVec(name, help string, labels ...string) *aiCounterVec {
	return &aiCounterVec{name: name, help: help, values: map[string]uint64{}, labels: labels}
}

func (c *aiCounterVec) add(delta uint64, labelValues ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := counterLabelKey(labelValues)
	c.values[key] += delta
}

func counterLabelKey(labelValues []string) string {
	key := ""
	for i, value := range labelValues {
		if i > 0 {
			key += "\x00"
		}
		key += value
	}
	return key
}

func (c *aiCounterVec) render(w io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	keys := make([]string, 0, len(c.values))
	for key := range c.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		labelValues := splitLabelKey(key, len(c.labels))
		fmt.Fprintf(w, "%s{", c.name)
		for i, value := range labelValues {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, "%s=%q", c.labels[i], value)
		}
		fmt.Fprintf(w, "} %d\n", c.values[key])
	}
	if len(keys) == 0 {
		// Emit nothing until the first observation keeps /metrics noise-free.
		return
	}
}

func splitLabelKey(key string, count int) []string {
	values := make([]string, 0, count)
	current := ""
	for _, r := range key {
		if r == '\x00' {
			values = append(values, current)
			current = ""
			continue
		}
		current += string(r)
	}
	values = append(values, current)
	for len(values) < count {
		values = append(values, "")
	}
	return values
}

type aiHistogram struct {
	name    string
	help    string
	buckets []float64
	mu      sync.Mutex
	counts  []uint64 // cumulative per bucket, including +Inf last
	sum     float64
	count   uint64
}

func newAIHistogram(name, help string, buckets ...float64) *aiHistogram {
	return &aiHistogram{name: name, help: help, buckets: buckets, counts: make([]uint64, len(buckets)+1)}
}

func (h *aiHistogram) observe(seconds float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += seconds
	h.count++
	for i, bound := range h.buckets {
		if seconds <= bound {
			h.counts[i]++
		}
	}
	h.counts[len(h.buckets)]++
}

func (h *aiHistogram) render(w io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	for i, bound := range h.buckets {
		fmt.Fprintf(w, "%s_bucket{le=\"%s\"} %d\n", h.name, strconv.FormatFloat(bound, 'f', -1, 64), h.counts[i])
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", h.name, h.counts[len(h.buckets)])
	fmt.Fprintf(w, "%s_sum %s\n", h.name, strconv.FormatFloat(h.sum, 'f', 9, 64))
	fmt.Fprintf(w, "%s_count %d\n", h.name, h.count)
}

var (
	aiRunsTotal = newAICounterVec(
		"arda_ai_runs_total",
		"AI agent runs by terminal status.",
		"status",
	)
	aiToolExecutionsTotal = newAICounterVec(
		"arda_ai_tool_executions_total",
		"AI tool executions by status and risk tier.",
		"status", "risk",
	)
	aiLLMTokensTotal = newAICounterVec(
		"arda_ai_llm_tokens_total",
		"LLM tokens consumed by type (prompt, completion, total).",
		"type",
	)
	aiRunDuration = newAIHistogram(
		"arda_ai_run_duration_seconds",
		"AI agent loop wall-clock duration.",
		0.25, 0.5, 1, 2, 5, 10, 15, 30, 60, 120,
	)
)

// RenderAIMetrics appends the arda_ai_* metric family to /metrics.
func RenderAIMetrics(w io.Writer) {
	aiRunsTotal.render(w)
	aiToolExecutionsTotal.render(w)
	aiLLMTokensTotal.render(w)
	aiRunDuration.render(w)
}

// recordRunOutcome counts one terminal (or paused) run status.
func recordRunOutcome(status string) {
	aiRunsTotal.add(1, status)
}

// recordToolOutcome counts one finished tool execution.
func recordToolOutcome(status, risk string) {
	if risk == "" {
		risk = "unknown"
	}
	aiToolExecutionsTotal.add(1, status, risk)
}

// recordLLMUsage persists token usage reported by the model stream.
func recordLLMUsage(usage model.Usage) {
	if usage.TotalTokens <= 0 {
		return
	}
	aiLLMTokensTotal.add(uint64(usage.PromptTokens), "prompt")
	aiLLMTokensTotal.add(uint64(usage.CompletionTokens), "completion")
	aiLLMTokensTotal.add(uint64(usage.TotalTokens), "total")
}

// aiRunTimer measures agent loop wall-clock time; observe on defer.
type aiRunTimer struct {
	start time.Time
}

func startAIRunTimer() *aiRunTimer {
	return &aiRunTimer{start: time.Now()}
}

func (t *aiRunTimer) observe() {
	aiRunDuration.observe(time.Since(t.start).Seconds())
}
