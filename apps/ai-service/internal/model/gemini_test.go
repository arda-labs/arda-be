package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSuggestAPIFormat(t *testing.T) {
	cases := map[string]APIFormat{
		"claude-sonnet-5-5":       FormatAnthropicMessages,
		"anthropic/claude-opus-5": FormatAnthropicMessages,
		"gemini-3.8-flash":        FormatGoogleGemini,
		"gpt-5.6-sol":             FormatOpenAIResponses,
		"gpt-5.3-codex":           FormatOpenAIResponses,
		"grok-4.7":                FormatOpenAIResponses,
		"o3-mini":                 FormatOpenAIResponses,
		"gpt-4o":                  FormatChatCompletions,
		"deepseek-v4-pro":         FormatChatCompletions,
		"glm-5.3":                 FormatChatCompletions,
		"":                        FormatChatCompletions,
	}
	for id, want := range cases {
		if got := SuggestAPIFormat(id); got != want {
			t.Errorf("SuggestAPIFormat(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestReasoningBudgetValidationAndPrecedence(t *testing.T) {
	for budget, want := range map[int]bool{0: true, 1023: false, 1024: true, 64000: true, 64001: false, -1: false} {
		if ValidReasoningBudget(budget) != want {
			t.Errorf("ValidReasoningBudget(%d) != %v", budget, want)
		}
	}
	if thinkingBudget(EffortHigh, 3000) != 3000 || thinkingBudget(EffortHigh, 0) != 16384 || thinkingBudget(EffortDefault, 0) != 0 {
		t.Error("an explicit budget must win over the effort mapping, and no effort means no thinking")
	}
	if _, ok := (Config{ReasoningBudget: 500}).Resolve(); ok {
		t.Error("an out-of-range budget must not resolve")
	}

	request := buildAnthropicRequest("m", []Message{{Role: "user", Content: "hi"}}, nil, EffortLow, 20000)
	if request.Thinking == nil || request.Thinking.BudgetTokens != 20000 || request.MaxTokens < 24096 {
		t.Errorf("explicit budget not applied: %+v max=%d", request.Thinking, request.MaxTokens)
	}
}

func TestGeminiSchemaKeepsOnlySupportedKeywords(t *testing.T) {
	var schema any
	_ = json.Unmarshal([]byte(`{
		"$schema":"http://json-schema.org/draft-07/schema#",
		"type":"object","additionalProperties":false,
		"required":["q"],
		"properties":{
			"q":{"type":["string","null"],"description":"query","default":"x"},
			"tags":{"type":"array","items":{"type":"string","additionalProperties":false}}
		}}`), &schema)
	got := geminiSchema(schema).(map[string]any)
	if _, bad := got["$schema"]; bad {
		t.Error("$schema must be dropped")
	}
	if _, bad := got["additionalProperties"]; bad {
		t.Error("additionalProperties must be dropped")
	}
	q := got["properties"].(map[string]any)["q"].(map[string]any)
	if q["type"] != "string" || q["nullable"] != true || q["description"] != "query" {
		t.Errorf("type array not collapsed: %+v", q)
	}
	if _, bad := q["default"]; bad {
		t.Error("default is not supported by Gemini")
	}
	items := got["properties"].(map[string]any)["tags"].(map[string]any)["items"].(map[string]any)
	if _, bad := items["additionalProperties"]; bad {
		t.Error("nested additionalProperties must be dropped")
	}
}

func TestGeminiRequestConversion(t *testing.T) {
	signed := json.RawMessage(`[{"functionCall":{"name":"search","args":{"q":"x"}},"thoughtSignature":"SIG"}]`)
	request := buildGeminiRequest([]Message{
		{Role: "system", Content: "rules"},
		{Role: "user", Content: "hỏi"},
		{Role: "assistant", Content: "tra", ToolCalls: []ToolCall{{ID: "call_1", Name: "search", Arguments: `{"q":"x"}`}}, ProviderState: signed},
		{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true}`},
		{Role: "tool", ToolCallID: "call_1", Content: "plain text"},
		{Role: "system", Content: "answer now"},
	}, []ToolDef{{Name: "search", Description: "d", Parameters: json.RawMessage(`{"type":"object","additionalProperties":false}`)}}, EffortMedium, 0)

	if request.SystemInstruction == nil || len(request.SystemInstruction.Parts) != 1 {
		t.Fatalf("system instruction wrong: %+v", request.SystemInstruction)
	}
	encoded, _ := json.Marshal(request.Contents)
	got := string(encoded)
	for _, want := range []string{
		`"role":"model"`, `"thoughtSignature":"SIG"`, `"functionResponse":{"name":"search","response":{"ok":true}}`,
		`"response":{"result":"plain text"}`, `answer now`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("contents missing %s\n%s", want, got)
		}
	}
	// user, model, then ONE user turn holding both responses and the late system text.
	if strings.Count(got, `"role":"user"`) != 2 || strings.Count(got, `"role":"model"`) != 1 {
		t.Errorf("expected user/model/user turns: %s", got)
	}
	thinking, _ := request.GenerationConfig["thinkingConfig"].(map[string]any)
	if thinking["thinkingBudget"] != 8192 || thinking["includeThoughts"] != true {
		t.Errorf("thinking config wrong: %+v", request.GenerationConfig)
	}
	if strings.Contains(string(mustJSON(request.Tools)), "additionalProperties") {
		t.Error("tool schema must be sanitized")
	}
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func TestGeminiStream(t *testing.T) {
	var path, query, key string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, key = r.URL.Path, r.URL.RawQuery, r.Header.Get("x-goog-api-key")
		_, _ = io.Copy(io.Discard, r.Body)
		anthropicStream(w,
			`{"candidates":[{"content":{"parts":[{"text":"đang nghĩ","thought":true}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"text":"Xin chào"}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"search","args":{"q":"a"}},"thoughtSignature":"SIG"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4,"thoughtsTokenCount":6,"totalTokenCount":20}}`,
		)
	}))
	defer server.Close()

	var text, reasoning strings.Builder
	var calls []ToolCall
	var state json.RawMessage
	backend := NewBackend(Options{APIFormat: FormatGoogleGemini}, server.URL+"/v1", "gk", "gemini-x", server.Client())
	reason, usage, err := backend.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{
		OnTextDelta:      func(d string) { text.WriteString(d) },
		OnReasoningDelta: func(d string) { reasoning.WriteString(d) },
		OnToolCall:       func(c ToolCall) { calls = append(calls, c) },
		OnProviderState:  func(s json.RawMessage) { state = s },
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/models/gemini-x:streamGenerateContent" || query != "alt=sse" || key != "gk" {
		t.Errorf("request wrong: %q ?%q key=%q", path, query, key)
	}
	if text.String() != "Xin chào" || reasoning.String() != "đang nghĩ" {
		t.Errorf("text=%q reasoning=%q", text.String(), reasoning.String())
	}
	if len(calls) != 1 || calls[0].Name != "search" || calls[0].Arguments != `{"q":"a"}` || calls[0].ID != "call_1" || reason != "tool_calls" {
		t.Errorf("calls=%+v reason=%q", calls, reason)
	}
	if usage.PromptTokens != 10 || usage.CompletionTokens != 10 || usage.TotalTokens != 20 {
		t.Errorf("usage = %+v", usage)
	}
	if !strings.Contains(string(state), `"thoughtSignature":"SIG"`) {
		t.Errorf("function call signature must be kept for replay: %s", state)
	}
}

func TestGeminiPromptBlockedFailsTheRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		anthropicStream(w, `{"promptFeedback":{"blockReason":"SAFETY"}}`)
	}))
	defer server.Close()
	backend := NewBackend(Options{APIFormat: FormatGoogleGemini}, server.URL, "k", "m", server.Client())
	if _, _, err := backend.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{}); err == nil {
		t.Fatal("a blocked prompt must surface as an error, not an empty answer")
	}
}

func TestListModelsAcrossFormats(t *testing.T) {
	openAIShape := `{"data":[{"id":"gpt-5.5"},{"id":"jev-1.13"},{"id":"claude-opus-5"}]}`
	geminiShape := `{"models":[{"name":"models/gemini-3.8-flash","supportedGenerationMethods":["generateContent"]},{"name":"models/text-embedding","supportedGenerationMethods":["embedContent"]}]}`
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		if strings.Contains(r.URL.Path, "gemini") || r.Header.Get("x-goog-api-key") != "" {
			_, _ = w.Write([]byte(geminiShape))
			return
		}
		_, _ = w.Write([]byte(openAIShape))
	}))
	defer server.Close()

	for _, format := range []APIFormat{FormatChatCompletions, FormatOpenAIResponses, FormatAnthropicMessages} {
		backend := NewBackend(Options{APIFormat: format}, server.URL+"/v1", "k", "m", server.Client())
		ids, err := backend.ListModels(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if !reflect.DeepEqual(ids, []string{"claude-opus-5", "gpt-5.5"}) {
			t.Errorf("%s: decision models must be filtered and IDs sorted, got %v", format, ids)
		}
	}
	gem := NewBackend(Options{APIFormat: FormatGoogleGemini}, server.URL+"/v1", "k", "m", server.Client())
	ids, err := gem.ListModels(context.Background())
	if err != nil || !reflect.DeepEqual(ids, []string{"gemini-3.8-flash"}) {
		t.Fatalf("gemini list: %v %v", ids, err)
	}
}

// An alias resolves server-side; every format must surface the versioned ID
// that actually answered so the audit trail can record it.
func TestServedModelIsReportedForEveryFormat(t *testing.T) {
	frames := map[APIFormat][]string{
		FormatChatCompletions:   {`{"model":"deepseek-v4-pro-0912","choices":[{"delta":{"content":"x"}}]}`},
		FormatAnthropicMessages: {`{"type":"message_start","message":{"model":"claude-sonnet-5-5-20260915","usage":{}}}`, `{"type":"message_stop"}`},
		FormatOpenAIResponses:   {`{"type":"response.created","response":{"model":"gpt-5.5-2026-09-01"}}`, `{"type":"response.completed","response":{}}`},
		FormatGoogleGemini:      {`{"modelVersion":"gemini-3.8-flash-002","candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"STOP"}]}`},
	}
	want := map[APIFormat]string{
		FormatChatCompletions: "deepseek-v4-pro-0912", FormatAnthropicMessages: "claude-sonnet-5-5-20260915",
		FormatOpenAIResponses: "gpt-5.5-2026-09-01", FormatGoogleGemini: "gemini-3.8-flash-002",
	}
	for format, lines := range frames {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			anthropicStream(w, lines...)
		}))
		served := ""
		backend := NewBackend(Options{APIFormat: format}, server.URL, "k", "alias", server.Client())
		_, _, err := backend.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, StreamCallbacks{
			OnServedModel: func(model string) { served = model },
		})
		server.Close()
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if served != want[format] {
			t.Errorf("%s: served model = %q, want %q", format, served, want[format])
		}
	}
}
