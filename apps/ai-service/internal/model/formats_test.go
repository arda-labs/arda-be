package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeAPIFormat(t *testing.T) {
	cases := map[string]APIFormat{
		"": FormatChatCompletions, "chat_completions": FormatChatCompletions,
		"anthropic_messages": FormatAnthropicMessages, " OPENAI_RESPONSES ": FormatOpenAIResponses,
	}
	for in, want := range cases {
		if got, ok := NormalizeAPIFormat(in); !ok || got != want {
			t.Errorf("NormalizeAPIFormat(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := NormalizeAPIFormat("graphql"); ok {
		t.Error("unknown formats must be rejected")
	}
	if _, ok := NormalizeReasoningEffort("extreme"); ok {
		t.Error("unknown effort must be rejected")
	}
}

func TestEndpointURL(t *testing.T) {
	cases := []struct {
		base, path string
		versioned  bool
		want       string
	}{
		{"https://api.anthropic.com", "/messages", true, "https://api.anthropic.com/v1/messages"},
		{"https://opencode.ai/zen/v1/", "/messages", true, "https://opencode.ai/zen/v1/messages"},
		{"https://gw.example/anthropic", "/messages", true, "https://gw.example/anthropic/v1/messages"},
		{"https://api.openai.com/v1", "/responses", false, "https://api.openai.com/v1/responses"},
	}
	for _, tc := range cases {
		if got := endpointURL(tc.base, tc.path, tc.versioned); got != tc.want {
			t.Errorf("endpointURL(%q,%q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

func TestThinkFilterSplitsLeadingBlockAcrossDeltas(t *testing.T) {
	var f thinkFilter
	var text, reasoning strings.Builder
	push := func(delta string) {
		out, r := f.feed(delta)
		text.WriteString(out)
		reasoning.WriteString(r)
	}
	for _, delta := range []string{"<th", "ink>cân nhắc ", "bước 1</thi", "nk>\n\nĐáp án", " là 42."} {
		push(delta)
	}
	t2, r2 := f.flush()
	text.WriteString(t2)
	reasoning.WriteString(r2)
	if reasoning.String() != "cân nhắc bước 1" {
		t.Errorf("reasoning = %q", reasoning.String())
	}
	if text.String() != "Đáp án là 42." {
		t.Errorf("text = %q", text.String())
	}
}

func TestThinkFilterLeavesPlainAnswersAlone(t *testing.T) {
	var f thinkFilter
	var text strings.Builder
	for _, delta := range []string{"Thẻ <think> chỉ là ", "ví dụ trong câu."} {
		out, r := f.feed(delta)
		if r != "" {
			t.Errorf("unexpected reasoning %q", r)
		}
		text.WriteString(out)
	}
	t2, _ := f.flush()
	text.WriteString(t2)
	if text.String() != "Thẻ <think> chỉ là ví dụ trong câu." {
		t.Errorf("text = %q", text.String())
	}
}

func TestChatCompletionsReasoningAliasesAndEffort(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range []string{
			`{"choices":[{"delta":{"reasoning":"alias "}}]}`,
			`{"choices":[{"delta":{"reasoning_details":[{"text":"details"}]}}]}`,
			`{"choices":[{"delta":{"content":"<think>inline</think>xong"}}]}`,
		} {
			_, _ = w.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var reasoning, text strings.Builder
	backend := NewBackend(Options{ProviderType: ProviderOpenAICompatible, APIFormat: FormatChatCompletions, ReasoningEffort: EffortHigh}, server.URL, "k", "m", server.Client())
	_, _, err := backend.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{
		OnReasoningDelta: func(d string) { reasoning.WriteString(d) },
		OnTextDelta:      func(d string) { text.WriteString(d) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v", body["reasoning_effort"])
	}
	if reasoning.String() != "alias detailsinline" || text.String() != "xong" {
		t.Errorf("reasoning=%q text=%q", reasoning.String(), text.String())
	}
}

func TestAnthropicRequestConversion(t *testing.T) {
	thinking := json.RawMessage(`[{"type":"thinking","thinking":"hmm","signature":"sig"}]`)
	request := buildAnthropicRequest("claude-x", []Message{
		{Role: "system", Content: "rules"},
		{Role: "system", Content: "sdk types"},
		{Role: "user", Content: "hỏi"},
		{Role: "assistant", Content: "đang tra", ToolCalls: []ToolCall{{ID: "tu_1", Name: "search", Arguments: `{"q":"x"}`}}, ProviderState: thinking},
		{Role: "tool", ToolCallID: "tu_1", Content: `{"ok":true}`},
		{Role: "system", Content: "write the final answer"},
	}, []ToolDef{{Name: "search", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}, EffortMedium, 0)

	if len(request.System) != 2 || request.System[1].CacheControl == nil || request.System[0].CacheControl != nil {
		t.Fatalf("system blocks / cache control wrong: %+v", request.System)
	}
	if request.Thinking == nil || request.Thinking.BudgetTokens != 8192 || request.MaxTokens < 8192+4096 {
		t.Fatalf("thinking budget / max_tokens wrong: %+v max=%d", request.Thinking, request.MaxTokens)
	}
	encoded, _ := json.Marshal(request.Messages)
	got := string(encoded)
	// user, assistant (thinking replay first, then text, then tool_use), then one
	// user turn carrying the tool_result followed by the late system text.
	for _, want := range []string{
		`"role":"user","content":[{"text":"hỏi","type":"text"}]`,
		`{"type":"thinking","thinking":"hmm","signature":"sig"}`,
		`"type":"tool_use"`, `"id":"tu_1"`,
		`"type":"tool_result"`, `"tool_use_id":"tu_1"`,
		`write the final answer`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("messages missing %s\n%s", want, got)
		}
	}
	if strings.Count(got, `"role":"user"`) != 2 || strings.Count(got, `"role":"assistant"`) != 1 {
		t.Errorf("expected user/assistant/user turns, got %s", got)
	}
	if strings.Index(got, "tool_result") > strings.Index(got, "write the final answer") {
		t.Error("tool_result must precede the late system text inside the user turn")
	}
}

func anthropicStream(w http.ResponseWriter, frames ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, frame := range frames {
		_, _ = w.Write([]byte("data: " + frame + "\n\n"))
	}
}

func TestAnthropicStreamTextThinkingAndToolUse(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotVersion = r.URL.Path, r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		_, _ = io.Copy(io.Discard, r.Body)
		anthropicStream(w,
			`{"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":50}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"suy "}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"nghĩ"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Xin chào"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tu_9","name":"search"}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
			`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"abc\"}"}}`,
			`{"type":"content_block_stop","index":2}`,
			`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":30}}`,
			`{"type":"message_stop"}`,
		)
	}))
	defer server.Close()

	var text, reasoning strings.Builder
	var calls []ToolCall
	var state json.RawMessage
	backend := NewBackend(Options{ProviderType: ProviderOpenAICompatible, APIFormat: FormatAnthropicMessages}, server.URL+"/v1", "secret", "claude-x", server.Client())
	reason, usage, err := backend.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{
		OnTextDelta:      func(d string) { text.WriteString(d) },
		OnReasoningDelta: func(d string) { reasoning.WriteString(d) },
		OnToolCall:       func(c ToolCall) { calls = append(calls, c) },
		OnProviderState:  func(s json.RawMessage) { state = s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/messages" || gotKey != "secret" || gotVersion != anthropicVersion {
		t.Errorf("request wrong: path=%q key=%q version=%q", gotPath, gotKey, gotVersion)
	}
	if text.String() != "Xin chào" || reasoning.String() != "suy nghĩ" {
		t.Errorf("text=%q reasoning=%q", text.String(), reasoning.String())
	}
	if len(calls) != 1 || calls[0].ID != "tu_9" || calls[0].Arguments != `{"q":"abc"}` {
		t.Errorf("tool calls = %+v", calls)
	}
	if reason != "tool_calls" {
		t.Errorf("finish reason = %q", reason)
	}
	if usage.PromptTokens != 150 || usage.CompletionTokens != 30 || usage.TotalTokens != 180 {
		t.Errorf("usage = %+v", usage)
	}
	if !strings.Contains(string(state), `"signature":"SIG"`) || !strings.Contains(string(state), `"thinking":"suy nghĩ"`) {
		t.Errorf("thinking block must be captured for replay: %s", state)
	}
}

func TestAnthropicStreamErrorEventFailsTheRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		anthropicStream(w, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`)
	}))
	defer server.Close()
	backend := NewBackend(Options{APIFormat: FormatAnthropicMessages}, server.URL+"/v1", "k", "m", server.Client())
	_, _, err := backend.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{})
	if _, ok := err.(*ProviderStatusError); !ok {
		t.Fatalf("expected a provider error, got %v", err)
	}
}

func TestResponsesRequestConversionAndReplay(t *testing.T) {
	reasoning := json.RawMessage(`[{"type":"reasoning","id":"rs_1","encrypted_content":"enc","summary":[]}]`)
	request := buildResponsesRequest("gpt-x", []Message{
		{Role: "system", Content: "rules"},
		{Role: "user", Content: "hỏi"},
		{Role: "assistant", Content: "tra", ToolCalls: []ToolCall{{ID: "call_1", Name: "search", Arguments: `{"q":1}`}}, ProviderState: reasoning},
		{Role: "tool", ToolCallID: "call_1", Content: "kết quả"},
	}, []ToolDef{{Name: "search", Parameters: json.RawMessage(`{"type":"object"}`)}}, EffortLow, "sess")

	if request.Instructions != "rules" || request.Store || request.PromptCacheKey != "sess" {
		t.Errorf("request header fields wrong: %+v", request)
	}
	if request.Reasoning == nil || request.Reasoning.Effort != "low" || len(request.Include) != 1 {
		t.Errorf("reasoning options wrong: %+v include=%v", request.Reasoning, request.Include)
	}
	encoded, _ := json.Marshal(request.Input)
	got := string(encoded)
	order := []string{`"rs_1"`, `"output_text"`, `"function_call"`, `"function_call_output"`}
	last := -1
	for _, want := range order {
		index := strings.Index(got, want)
		if index < 0 || index < last {
			t.Fatalf("input order wrong for %s: %s", want, got)
		}
		last = index
	}
	if !strings.Contains(got, `"call_id":"call_1"`) || !strings.Contains(got, `"arguments":"{\"q\":1}"`) {
		t.Errorf("function call encoding wrong: %s", got)
	}
}

func TestResponsesStream(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = io.Copy(io.Discard, r.Body)
		anthropicStream(w,
			`{"type":"response.reasoning_summary_text.delta","delta":"tóm tắt"}`,
			`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_1","encrypted_content":"enc"}}`,
			`{"type":"response.output_text.delta","delta":"Kết quả"}`,
			`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"c1","name":"search","arguments":"{\"q\":\"a\"}"}}`,
			`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`,
		)
	}))
	defer server.Close()

	var text, reasoning strings.Builder
	var calls []ToolCall
	var state json.RawMessage
	backend := NewBackend(Options{APIFormat: FormatOpenAIResponses}, server.URL+"/v1", "k", "gpt-x", server.Client())
	reason, usage, err := backend.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{
		OnTextDelta:      func(d string) { text.WriteString(d) },
		OnReasoningDelta: func(d string) { reasoning.WriteString(d) },
		OnToolCall:       func(c ToolCall) { calls = append(calls, c) },
		OnProviderState:  func(s json.RawMessage) { state = s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/responses" || text.String() != "Kết quả" || reasoning.String() != "tóm tắt" {
		t.Errorf("path=%q text=%q reasoning=%q", path, text.String(), reasoning.String())
	}
	if len(calls) != 1 || calls[0].ID != "c1" || calls[0].Arguments != `{"q":"a"}` || reason != "tool_calls" {
		t.Errorf("calls=%+v reason=%q", calls, reason)
	}
	if usage.TotalTokens != 15 || !strings.Contains(string(state), `"encrypted_content":"enc"`) {
		t.Errorf("usage=%+v state=%s", usage, state)
	}
}

func TestResponsesIncompleteMapsToLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		anthropicStream(w,
			`{"type":"response.output_text.delta","delta":"cụt"}`,
			`{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`,
		)
	}))
	defer server.Close()
	backend := NewBackend(Options{APIFormat: FormatOpenAIResponses}, server.URL, "k", "m", server.Client())
	reason, _, err := backend.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{})
	if err != nil || reason != "length" {
		t.Fatalf("reason=%q err=%v", reason, err)
	}
}

func TestPoolSeparatesFormatsForTheSameEndpoint(t *testing.T) {
	pool := NewClientPool(nil)
	chat := pool.GetClient("t", Config{ProviderType: "openai-compatible"}, "https://x/v1", "k", "m")
	messages := pool.GetClient("t", Config{ProviderType: "openai-compatible", APIFormat: "anthropic_messages"}, "https://x/v1", "k", "m")
	if chat == messages {
		t.Fatal("different API formats must not share a pooled client")
	}
	if pool.GetProvider("t", Config{APIFormat: "nope"}, "https://x/v1", "k", "m") != nil {
		t.Fatal("an unknown format must not produce a provider")
	}
}
