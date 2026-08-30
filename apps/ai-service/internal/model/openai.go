package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	// Reasoning carries provider chain-of-thought (reasoning_content) so a
	// thinking-mode assistant turn can be replayed within the same run —
	// thinking providers (deepseek et al.) reject the follow-up request
	// without it.
	Reasoning  string     `json:"reasoning_content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolCallWire struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	type alias struct {
		Role    string          `json:"role"`
		Content string          `json:"content,omitempty"`
		ToolCalls []toolCallWire `json:"tool_calls,omitempty"`
		ToolCallID string        `json:"tool_call_id,omitempty"`
	}
	out := alias{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
	for _, call := range m.ToolCalls {
		wire := toolCallWire{ID: call.ID, Type: "function"}
		wire.Function.Name = call.Name
		wire.Function.Arguments = call.Arguments
		out.ToolCalls = append(out.ToolCalls, wire)
	}
	return json.Marshal(out)
}

type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

const defaultTimeout = 120 * time.Second

// Provider abstracts the streaming chat backend so the handler can later
// route between multiple sources (cloud, local vLLM/Ollama) without changes.
type Provider interface {
	StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (finishReason string, usage Usage, err error)
}

var _ Provider = (*Client)(nil)

type Client struct {
	baseURL      string
	apiKey       string
	model        string
	gatewayToken string
	http         *http.Client
}

// WithGatewayToken sets an AI Gateway credential sent as the
// cf-aig-authorization header, separate from the upstream provider key in
// Authorization (Cloudflare AI Gateway authentication mode).
func (c *Client) WithGatewayToken(token string) *Client {
	c.gatewayToken = strings.TrimSpace(token)
	return c
}

func NewClient(baseURL, apiKey, model string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  apiKey,
		model:   model,
		http:    httpClient,
	}
}

type streamRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []toolSchema `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

type toolSchema struct {
	Type     string          `json:"type"`
	Function toolDefinition  `json:"function"`
}

type toolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type StreamCallbacks struct {
	OnTextDelta   func(delta string)
	OnToolCall    func(call ToolCall)
	OnFinish      func(reason string, usage Usage)
	// OnReasoningDelta surfaces provider chain-of-thought deltas
	// (e.g. deepseek reasoning_content) for reasoning-aware clients.
	OnReasoningDelta func(delta string)
}

// StreamChat sends a chat completion request and consumes the SSE stream.
// It returns the finish reason of the last choice plus token usage when the
// provider reports it.
func (c *Client) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (string, Usage, error) {
	if c == nil || c.baseURL == "" || c.model == "" {
		return "", Usage{}, fmt.Errorf("model client is not configured")
	}
	request := streamRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   true,
	}
	request.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}
	for _, tool := range tools {
		parameters := tool.Parameters
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		request.Tools = append(request.Tools, toolSchema{
			Type: "function",
			Function: toolDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  parameters,
			},
		})
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return "", Usage{}, fmt.Errorf("encode model request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", Usage{}, fmt.Errorf("create model request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if c.gatewayToken != "" {
		req.Header.Set("cf-aig-authorization", "Bearer "+c.gatewayToken)
	}

	response, err := c.http.Do(req)
	if err != nil {
		return "", Usage{}, fmt.Errorf("model request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return "", Usage{}, fmt.Errorf("model returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	finishReason := ""
	var usage Usage
	pending := newPendingToolCalls()

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		for _, choice := range chunk.Choices {
			delta := choice.Delta
			if delta.Content != "" && callbacks.OnTextDelta != nil {
				callbacks.OnTextDelta(delta.Content)
			}
			if delta.Reasoning != "" && callbacks.OnReasoningDelta != nil {
				callbacks.OnReasoningDelta(delta.Reasoning)
			}
			for _, raw := range delta.ToolCalls {
				call, complete := pending.add(raw)
				if complete && callbacks.OnToolCall != nil {
					callbacks.OnToolCall(call)
				}
			}
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return finishReason, usage, fmt.Errorf("read model stream: %w", err)
	}
	if callbacks.OnFinish != nil {
		callbacks.OnFinish(finishReason, usage)
	}
	return finishReason, usage, nil
}

	type streamChunk struct {
		Choices []struct {
			Delta struct {
				Content   string         `json:"content"`
				Reasoning string         `json:"reasoning_content"`
				ToolCalls []toolCallWire `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *Usage `json:"usage"`
	}

type pendingToolCalls struct {
	order []*toolAccumulator
	items map[int]*toolAccumulator
}

type toolAccumulator struct {
	index     int
	id        string
	name      string
	arguments strings.Builder
}

func newPendingToolCalls() *pendingToolCalls {
	return &pendingToolCalls{items: map[int]*toolAccumulator{}}
}

func (p *pendingToolCalls) add(raw toolCallWire) (ToolCall, bool) {
	item, ok := p.items[raw.Index]
	if !ok {
		item = &toolAccumulator{index: raw.Index}
		p.items[raw.Index] = item
		p.order = append(p.order, item)
	}
	if raw.ID != "" && item.id == "" {
		item.id = raw.ID
	}
	if raw.Function.Name != "" && item.name == "" {
		item.name = raw.Function.Name
	}
	if raw.Function.Arguments != "" {
		item.arguments.WriteString(raw.Function.Arguments)
	}
	if item.id == "" || item.name == "" || !validJSON(item.arguments.String()) {
		return ToolCall{}, false
	}
	delete(p.items, raw.Index)
	return ToolCall{ID: item.id, Name: item.name, Arguments: item.arguments.String()}, true
}

func validJSON(value string) bool {
	if value == "" {
		return false
	}
	var out any
	return json.Unmarshal([]byte(value), &out) == nil
}
