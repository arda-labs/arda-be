package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// Golden fixtures for the AG-UI protocol (agent-gui SSE events consumed by
// @assistant-ui/react-ag-ui).

func runAgentStreamFixture(t *testing.T, store runStore, resolver toolResolver, options RouterOptions, body string) (int, []map[string]any) {
	t.Helper()
	router := NewRouterWithOptions(store, resolver, options)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(body))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res.Code, decodeSSEEvents(t, res.Body.String())
}

func TestStream_TextOnlyFixture(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"content":"Xin chào"}}]}`,
		`{"choices":[{"delta":{"content":" khách hàng!"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}})
	defer server.Close()

	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client())}
	code, events := runAgentStreamFixture(t, &agentRunStore{}, tools.NewRegistry(handlerTestTool{}), options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}

	types := eventTypes(events)
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if len(types) != len(want) {
		t.Fatalf("event sequence mismatch: %v", types)
	}
	for i, name := range want {
		if types[i] != name {
			t.Fatalf("event %d: expected %s, got %s (all: %v)", i, name, types[i], types)
		}
	}
	joined := events[2]["delta"].(string) + events[3]["delta"].(string)
	if joined != "Xin chào khách hàng!" {
		t.Fatalf("text deltas do not concatenate: %q", joined)
	}
	if events[1]["messageId"] != events[4]["messageId"] { // text start id == end id
		t.Fatalf("text start/end messageId mismatch")
	}
	// Success outcome must be present on the terminal RUN_FINISHED.
	outcome, ok := events[5]["outcome"].(map[string]any)
	if !ok || outcome["type"] != "success" {
		t.Fatalf("RUN_FINISHED outcome wrong: %v", events[5]["outcome"])
	}
}

func TestStream_ToolCallFixture(t *testing.T) {
	server := newModelServer(t, [][]string{
		{
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"test.read"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"customer"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"Id\":\"c1\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		},
		{
			`{"choices":[{"delta":{"content":"Xong."}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		},
	})
	defer server.Close()

	store := &agentRunStore{}
	resolver := tools.NewRegistry(handlerTestTool{})
	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client()), AgentMaxSteps: 3}
	_, events := runAgentStreamFixture(t, store, resolver, options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"xem khách hàng"}]}`)

	var callStart, callResult map[string]any
	for _, event := range events {
		switch event["type"] {
		case "TOOL_CALL_START":
			callStart = event
		case "TOOL_CALL_RESULT":
			callResult = event
		}
	}
	if callStart == nil {
		t.Fatalf("missing TOOL_CALL_START in %v", eventTypes(events))
	}
	if callStart["toolCallId"] != "call-1" || callStart["toolCallName"] != "test.read" {
		t.Fatalf("TOOL_CALL_START fields wrong: %v", callStart)
	}
	if callResult == nil {
		t.Fatalf("missing TOOL_CALL_RESULT")
	}
	// handlerTestTool returns Data {"id":"customer-1"} + Summary; the agent
	// loop wraps it as {"summary":...,"data":{...}}.
	content, _ := callResult["content"].(string)
	if !strings.Contains(content, "customer-1") || !strings.Contains(content, "summary") {
		t.Fatalf("tool result content wrong: %q", content)
	}
	if callResult["role"] != "tool" {
		t.Fatalf("tool result role wrong: %v", callResult["role"])
	}
	if !store.toolStarted || !store.finished {
		t.Fatalf("tool persistence missing: %+v", store)
	}
}

func TestStream_HitlPendingFixture(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-9","type":"function","function":{"name":"test.confirm","arguments":"{\"format\":\"csv\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}})
	defer server.Close()

	store := &agentRunStore{}
	resolver := tools.NewRegistry(handlerConfirmTool{})
	options := RouterOptions{
		ModelProvider:       model.NewClient(server.URL, "k", "m", server.Client()),
		AgentMaxSteps:       3,
		EnableHITLProposals: true,
	}
	_, events := runAgentStreamFixture(t, store, resolver, options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"xuất dữ liệu"}]}`)

	var sawInterruptOutcome, sawProposal bool
	for _, event := range events {
		if event["type"] == "RUN_FINISHED" {
			outcome, _ := event["outcome"].(map[string]any)
			if outcome == nil || outcome["type"] != "interrupt" {
				continue
			}
			interrupts, _ := outcome["interrupts"].([]any)
			if len(interrupts) > 0 {
				if first, ok := interrupts[0].(map[string]any); ok && first["id"] != nil && first["reason"] == "confirmation" && first["toolCallId"] == "call-9" {
					sawInterruptOutcome = true
				}
			}
		}
		if event["type"] == "TOOL_CALL_RESULT" {
			if content, ok := event["content"].(string); ok && strings.Contains(content, "proposal") {
				sawProposal = true
			}
		}
	}
	if !sawProposal {
		t.Fatalf("proposal not surfaced as tool result; events: %v", eventTypes(events))
	}
	if !sawInterruptOutcome {
		t.Fatalf("interrupt outcome missing on RUN_FINISHED; events: %v", eventTypes(events))
	}
}

func TestStream_ReasoningParts(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"reasoning_content":"Đang phân tích"}}]}`,
		`{"choices":[{"delta":{"reasoning_content":" yêu cầu..."}}]}`,
		`{"choices":[{"delta":{"content":"Câu trả lời."}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}})
	defer server.Close()

	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client())}
	_, events := runAgentStreamFixture(t, &agentRunStore{}, tools.NewRegistry(handlerTestTool{}), options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`)

	var reasoningDelta, reasoningEnd string
	for _, event := range events {
		switch event["type"] {
		case "REASONING_MESSAGE_CONTENT":
			reasoningDelta += event["delta"].(string)
			reasoningEnd = "pending"
		case "REASONING_MESSAGE_END":
			reasoningEnd = event["messageId"].(string)
		}
	}
	if reasoningDelta != "Đang phân tích yêu cầu..." {
		t.Fatalf("reasoning deltas wrong: %q", reasoningDelta)
	}
	if reasoningEnd == "" || reasoningEnd == "pending" {
		t.Fatalf("REASONING_MESSAGE_END missing (events: %v)", eventTypes(events))
	}
	if !strings.Contains(reasoningDelta, "yêu cầu") {
		t.Fatalf("unexpected reasoning content")
	}
}

func TestStream_ModelErrorEmitsRunError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer server.Close()

	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client())}
	_, events := runAgentStreamFixture(t, &agentRunStore{}, tools.NewRegistry(handlerTestTool{}), options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`)

	var sawRunError bool
	for _, event := range events {
		if event["type"] == "RUN_ERROR" {
			if msg, _ := event["message"].(string); msg != "" {
				sawRunError = true
			}
		}
	}
	if !sawRunError {
		t.Fatalf("expected RUN_ERROR event; events: %v", eventTypes(events))
	}
	for _, event := range events {
		if event["type"] == "RUN_FINISHED" {
			t.Fatalf("RUN_ERROR is terminal; no RUN_FINISHED should follow: %v", eventTypes(events))
		}
	}
}