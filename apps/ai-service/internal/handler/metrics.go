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
// enough for the aggregate views this service exposes.

type aiCounterVec struct {
	name string
	help string
	mu   sync.Mutex
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
	aiProviderProbesTotal = newAICounterVec(
		"arda_ai_provider_probes_total",
		"Model provider health probes by provider and outcome.",
		"provider", "outcome",
	)
	aiModelErrorsTotal = newAICounterVec(
		"arda_ai_model_errors_total",
		"Model stream failures by actionable error code.",
		"code",
	)
	aiCitationGuardTotal = newAICounterVec(
		"arda_ai_citation_guard_total",
		"Knowledge answers by citation outcome (present, appended).",
		"outcome",
	)
	aiInventedCitationsTotal = newAICounterVec(
		"arda_ai_invented_citations_total",
		"Fabricated [source]/[citation]/[chunk] tokens removed from persisted answers.",
		"kind",
	)
	// Segment latency families (audit-2026-09 A3): the run duration alone could
	// not tell model latency apart from retrieval or domain latency, which made
	// the 2026-09 knowledge-search outage hard to localise.
	aiModelTTFT = newAIHistogram(
		"arda_ai_model_ttft_seconds",
		"Time to the first streamed model delta (text, reasoning, or tool call).",
		0.1, 0.25, 0.5, 1, 1.5, 2, 3, 5, 8, 15,
	)
	aiModelDuration = newAIHistogram(
		"arda_ai_model_duration_seconds",
		"Model stream wall-clock duration per turn.",
		0.25, 0.5, 1, 2, 3, 5, 10, 15, 30, 60,
	)
	aiRetrievalEmbedDuration = newAIHistogram(
		"arda_ai_retrieval_embed_seconds",
		"Query embedding round-trip duration (external provider).",
		0.1, 0.25, 0.5, 1, 1.5, 2, 3, 5, 8, 15,
	)
	aiRetrievalSearchDuration = newAIHistogram(
		"arda_ai_retrieval_search_seconds",
		"Hybrid (vector + full-text) knowledge search duration.",
		0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5,
	)
	aiEventPublishFailures = newAICounterVec(
		"arda_ai_event_publish_failures_total",
		"NATS event publishes that failed and fell back to the in-process buffer.",
		"subject",
	)
	// Context-budget families (audit-2026-09 A6): the measured prompt growth
	// came from replayed history and tool results, not the SDK header.
	aiPromptBytes = newAIHistogram(
		"arda_ai_prompt_bytes",
		"Approximate prompt size (message content bytes) sent to the model per turn.",
		1024, 4096, 8192, 16384, 32768, 65536, 131072, 262144,
	)
	aiContextTruncatedTotal = newAICounterVec(
		"arda_ai_context_truncated_total",
		"Context items dropped or truncated to fit the prompt budget.",
		"kind",
	)
)

// RenderAIMetrics appends the arda_ai_* metric family to /metrics.
func RenderAIMetrics(w io.Writer) {
	aiRunsTotal.render(w)
	aiToolExecutionsTotal.render(w)
	aiLLMTokensTotal.render(w)
	aiRunDuration.render(w)
	aiProviderProbesTotal.render(w)
	aiModelErrorsTotal.render(w)
	aiCitationGuardTotal.render(w)
	aiInventedCitationsTotal.render(w)
	aiModelTTFT.render(w)
	aiModelDuration.render(w)
	aiRetrievalEmbedDuration.render(w)
	aiRetrievalSearchDuration.render(w)
	aiEventPublishFailures.render(w)
	aiPromptBytes.render(w)
	aiContextTruncatedTotal.render(w)
}

// RecordProviderProbe records one readiness health probe without exposing
// provider credentials or URLs.
func RecordProviderProbe(provider string, err error) {
	if provider == "" {
		provider = "unknown"
	}
	outcome := "success"
	if err != nil {
		outcome = "failure"
	}
	aiProviderProbesTotal.add(1, provider, outcome)
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

// recordModelError counts one model stream failure by actionable code.
func recordModelError(code string) {
	if code == "" {
		code = "ai.model_unavailable"
	}
	aiModelErrorsTotal.add(1, code)
}

// recordCitationGuard counts whether an answer already cited retrieved
// evidence or the guard had to append the source list.
func recordCitationGuard(outcome string) {
	aiCitationGuardTotal.add(1, outcome)
}

// recordInventedCitations counts fabricated citation tokens removed from the
// persisted answer, so the rate is visible even though streaming cannot
// retract an already-sent delta.
func recordInventedCitations(count int) {
	if count > 0 {
		aiInventedCitationsTotal.add(uint64(count), "bracket")
	}
}

// recordRemovedSources counts source-section items removed because they did
// not match any retrieved citation, or a whole section removed from a
// no-evidence answer (audit-2026-09 A10 follow-up).
func recordRemovedSources(kind string, count int) {
	if count > 0 {
		aiInventedCitationsTotal.add(uint64(count), kind)
	}
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

func (t *aiRunTimer) durationMs() int64 {
	return time.Since(t.start).Milliseconds()
}

// modelStreamTimer measures one StreamChat call: time to the first delta and
// total stream duration. Callbacks run on the StreamChat goroutine, so no
// locking is required.
type modelStreamTimer struct {
	start time.Time
	first time.Duration
}

func startModelStreamTimer() *modelStreamTimer {
	return &modelStreamTimer{start: time.Now()}
}

// firstDelta records the first streamed output of any kind. Later calls are
// ignored.
func (t *modelStreamTimer) firstDelta() {
	if t.first == 0 {
		t.first = time.Since(t.start)
	}
}

func (t *modelStreamTimer) observe() {
	if t.first > 0 {
		aiModelTTFT.observe(t.first.Seconds())
	}
	aiModelDuration.observe(time.Since(t.start).Seconds())
}

// RecordRetrievalStage records one knowledge-retrieval stage duration for
// /metrics. The knowledge service calls it through SetStageObserver so the two
// packages do not import each other.
func RecordRetrievalStage(stage string, d time.Duration) {
	switch stage {
	case "embed":
		aiRetrievalEmbedDuration.observe(d.Seconds())
	case "search":
		aiRetrievalSearchDuration.observe(d.Seconds())
	case "rerank":
		// Reranking is optional and currently unconfigured in production; the
		// stage is accepted so enabling it does not require a new hook.
	}
}

// RecordEventPublishFailure counts one event that could not reach the durable
// NATS stream and fell back to the in-process buffer (audit-2026-09 A3: the
// previous WARN-only path left the outage invisible).
func RecordEventPublishFailure(subject string) {
	if subject == "" {
		subject = "unknown"
	}
	aiEventPublishFailures.add(1, subject)
}

// recordPromptSize observes the approximate prompt size of one model turn.
func recordPromptSize(messages []model.Message) {
	total := 0
	for _, message := range messages {
		total += len(message.Content) + len(message.Reasoning)
		for _, call := range message.ToolCalls {
			total += len(call.Arguments)
		}
	}
	aiPromptBytes.observe(float64(total))
}

// recordContextTruncated counts dropped or truncated context items by kind
// (history, tool_result) so the compaction rate is visible.
func recordContextTruncated(kind string) {
	if kind == "" {
		kind = "unknown"
	}
	aiContextTruncatedTotal.add(1, kind)
}
