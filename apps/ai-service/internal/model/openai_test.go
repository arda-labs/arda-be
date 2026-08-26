package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamChatParsesTextDeltasToolCallsAndUsage(t *testing.T) {
	var sseBody strings.Builder
	sseBody.WriteString(`data: {"choices":[{"delta":{"content":"Xin "}}]}` + "\n\n")
	sseBody.WriteString(`data: {"choices":[{"delta":{"content":"chào"}}]}` + "\n\n")
	sseBody.WriteString(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"crm.customer.get","arguments":""}}]}}]}` + "\n\n")
	sseBody.WriteString(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"customer"}}]}}]}` + "\n\n")
	sseBody.WriteString(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"Id\":\"c1\"}"}}]}}]}` + "\n\n")
	sseBody.WriteString(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n")
	sseBody.WriteString(`data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}` + "\n\n")
	sseBody.WriteString("data: [DONE]\n\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseBody.String()))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "test-model", server.Client())
	var texts []string
	var calls []ToolCall
	finish := ""
	var finishUsage Usage

	reason, usageResult, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{
		OnTextDelta: func(delta string) { texts = append(texts, delta) },
		OnToolCall: func(call ToolCall) { calls = append(calls, call) },
		OnFinish: func(reason string, usage Usage) {
			finish = reason
			finishUsage = usage
		},
	})
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	if got := strings.Join(texts, ""); got != "Xin chào" {
		t.Fatalf("unexpected text deltas: %q", got)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 completed tool call, got %d", len(calls))
	}
	if calls[0].ID != "call-1" || calls[0].Name != "crm.customer.get" || calls[0].Arguments != `{"customerId":"c1"}` {
		t.Fatalf("unexpected tool call: %+v", calls[0])
	}
	if reason != "tool_calls" || finish != "tool_calls" {
		t.Fatalf("unexpected finish reason: %q / %q", reason, finish)
	}
	if usageResult.TotalTokens != 18 || finishUsage.TotalTokens != 18 {
		t.Fatalf("unexpected usage: %+v / %+v", usageResult, finishUsage)
	}
}

func TestStreamChatReportsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "wrong", "m", server.Client())
	_, _, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{})
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("expected provider status error, got %v", err)
	}
}
