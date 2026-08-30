package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arda-labs/arda/apps/ai-service/internal/model"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// Golden fixtures for the AI SDK UI Message Stream v1 protocol.

func runAgentStreamFixture(t *testing.T, store runStore, resolver toolResolver, options RouterOptions, body string) (int, string, []map[string]any) {
	t.Helper()
	router := NewRouterWithOptions(store, resolver, options)
	req := httptest.NewRequest(http.MethodPost, "/api/ai/agent", strings.NewReader(body))
	gatewayHeaders(req)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res.Code, res.Header().Get("x-vercel-ai-ui-message-stream"), decodeSSEEvents(t, res.Body.String())
}

func TestStream_TextOnlyFixture(t *testing.T) {
	server := newModelServer(t, [][]string{{
		`{"choices":[{"delta":{"content":"Xin chào"}}]}`,
		`{"choices":[{"delta":{"content":" khách hàng!"}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
	}})
	defer server.Close()

	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client())}
	code, header, events := runAgentStreamFixture(t, &agentRunStore{}, tools.NewRegistry(handlerTestTool{}), options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if header != "v1" {
		t.Fatalf("missing x-vercel-ai-ui-message-stream header, got %q", header)
	}

	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	types := eventTypes(events)
	want := []string{"start", "start-step", "text-start", "text-delta", "text-delta", "text-end", "finish-step", "finish"}
	if len(types) != len(want) {
		t.Fatalf("event sequence mismatch: %v", types)
	}
	for i, name := range want {
		if types[i] != name {
			t.Fatalf("part %d: expected %s, got %s (all: %v)", i, name, types[i], types)
		}
	}
	joined := events[3]["delta"].(string) + events[4]["delta"].(string)
	if joined != "Xin chào khách hàng!" {
		t.Fatalf("text deltas do not concatenate: %q", joined)
	}
	if events[2]["id"] != events[5]["id"] { // text-start id == text-end id
		t.Fatalf("text-start/text-end id mismatch")
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
	_, _, events := runAgentStreamFixture(t, store, resolver, options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"xem khách hàng"}]}`)

	var inputAvailable, outputAvailable map[string]any
	for _, event := range events {
		switch event["type"] {
		case "tool-input-available":
			inputAvailable = event
		case "tool-output-available":
			outputAvailable = event
		}
	}
	if inputAvailable == nil {
		t.Fatalf("missing tool-input-available in %v", eventTypes(events))
	}
	if inputAvailable["toolCallId"] != "call-1" || inputAvailable["toolName"] != "test.read" {
		t.Fatalf("tool-input-available fields wrong: %v", inputAvailable)
	}
	if input, _ := inputAvailable["input"].(map[string]any); input == nil || input["customerId"] != "c1" {
		t.Fatalf("streamed args not reassembled: %v", inputAvailable["input"])
	}
	if outputAvailable == nil {
		t.Fatalf("missing tool-output-available")
	}
	// handlerTestTool returns Data {"id":"customer-1"} + Summary; the writer
	// wraps it as {"summary":...,"data":{...}}.
	output, _ := outputAvailable["output"].(map[string]any)
	if output == nil || output["summary"] == nil {
		t.Fatalf("tool output wrong: %v", outputAvailable)
	}
	data, _ := output["data"].(map[string]any)
	if data == nil || data["id"] != "customer-1" {
		t.Fatalf("tool output data wrong: %v", outputAvailable)
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
	_, _, events := runAgentStreamFixture(t, store, resolver, options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"xuất dữ liệu"}]}`)

	var sawProposal, sawApprovalRequest bool
	for _, event := range events {
		if event["type"] != "tool-output-available" && event["type"] != "tool-approval-request" {
			continue
		}
		if event["type"] == "tool-approval-request" {
			if id, _ := event["approvalId"].(string); id != "" && event["toolCallId"] == "call-9" {
				sawApprovalRequest = true
			}
			continue
		}
		output, _ := event["output"].(map[string]any)
		if proposal, ok := output["proposal"].(map[string]any); ok && proposal["id"] != nil {
			sawProposal = true
		}
	}
	if !sawProposal {
		t.Fatalf("proposal not surfaced as tool output; events: %v", eventTypes(events))
	}
	if !sawApprovalRequest {
		t.Fatalf("spec tool-approval-request part missing; events: %v", eventTypes(events))
	}
	for _, event := range events {
		if event["type"] == "finish" {
			return // stream must close cleanly while awaiting approval
		}
	}
	t.Fatalf("stream did not finish; events: %v", eventTypes(events))
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
	_, _, events := runAgentStreamFixture(t, &agentRunStore{}, tools.NewRegistry(handlerTestTool{}), options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`)

	var reasoningDelta, reasoningEnd string
	for _, event := range events {
		switch event["type"] {
		case "reasoning-delta":
			reasoningDelta += event["delta"].(string)
			reasoningEnd = "pending"
		case "reasoning-end":
			reasoningEnd = event["id"].(string)
		}
	}
	if reasoningDelta != "Đang phân tích yêu cầu..." {
		t.Fatalf("reasoning deltas wrong: %q", reasoningDelta)
	}
	if reasoningEnd == "" || reasoningEnd == "pending" {
		t.Fatalf("reasoning-end part missing (events: %v)", eventTypes(events))
	}
	if !strings.Contains(reasoningDelta, "yêu cầu") {
		t.Fatalf("unexpected reasoning content")
	}
}

func TestStream_ModelErrorEmitsErrorPart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer server.Close()

	options := RouterOptions{ModelProvider: model.NewClient(server.URL, "k", "m", server.Client())}
	_, _, events := runAgentStreamFixture(t, &agentRunStore{}, tools.NewRegistry(handlerTestTool{}), options,
		`{"threadId":"t1","runId":"r1","messages":[{"role":"user","content":"chào"}]}`)

	var sawError, sawFinish bool
	for _, event := range events {
		switch event["type"] {
		case "error":
			sawError = true
		case "finish":
			sawFinish = true
		}
	}
	if !sawError || !sawFinish {
		t.Fatalf("expected error part followed by finish; events: %v", eventTypes(events))
	}
}
