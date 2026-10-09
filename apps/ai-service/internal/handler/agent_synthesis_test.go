package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// The last step must be a tool-free synthesis turn so evidence gathered by
// earlier tool rounds becomes an answer instead of an ai.agent_step_limit
// failure.
func TestAgentFinalStepIsToolFreeSynthesis(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	toolTurn := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"test.read","arguments":"{\"customerId\":\"c1\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	finalTurn := []string{
		`{"choices":[{"delta":{"content":"Tổng hợp từ dữ liệu đã có."}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		index := len(bodies)
		mu.Unlock()
		lines := toolTurn
		if index >= 3 {
			lines = finalTurn
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			_, _ = w.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	store := &agentRunStore{}
	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client()), AgentMaxSteps: 3}
	router := NewRouterWithOptions(store, tools.NewRegistry(handlerTestTool{}), options)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"xem khách hàng"}]}`))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if strings.Contains(res.Body.String(), "ai.agent_step_limit") {
		t.Fatalf("run must finish with a synthesized answer, got: %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "Tổng hợp từ dữ liệu đã có.") {
		t.Fatalf("synthesis answer missing from stream: %s", res.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("expected 3 model requests, got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], `"tools"`) || !strings.Contains(bodies[1], `"tools"`) {
		t.Fatal("early steps must still offer tools")
	}
	if strings.Contains(bodies[2], `"tools"`) {
		t.Fatal("the final step must not offer tools")
	}
}
