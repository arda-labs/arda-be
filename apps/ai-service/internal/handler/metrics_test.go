package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

func TestAIMetricsRenderedAfterRun(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"content":"Trả lời."}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`,
	}})
	defer server.Close()

	store := &agentRunStore{}
	resolver := tools.NewRegistry(handlerTestTool{})
	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client())}
	router := NewRouterWithOptions(store, resolver, options)

	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	metricsBuffer := &strings.Builder{}
	RenderAIMetrics(metricsBuffer)
	metrics := metricsBuffer.String()
	// Counters are package-level and other tests in the package contribute
	// runs, so assert presence of the label lines rather than exact values.
	if !strings.Contains(metrics, `arda_ai_runs_total{status="SUCCEEDED"} `) {
		t.Fatalf("missing run counter in metrics:\n%s", metrics)
	}
	if !strings.Contains(metrics, `arda_ai_llm_tokens_total{type="prompt"} 11`) ||
		!strings.Contains(metrics, `arda_ai_llm_tokens_total{type="completion"} 7`) ||
		!strings.Contains(metrics, `arda_ai_llm_tokens_total{type="total"} 18`) {
		t.Fatalf("missing token counters in metrics:\n%s", metrics)
	}
	if !strings.Contains(metrics, "arda_ai_run_duration_seconds_count ") {
		t.Fatalf("missing duration histogram in metrics:\n%s", metrics)
	}
}
