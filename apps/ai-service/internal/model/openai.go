package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
	// Reasoning carries provider chain-of-thought (reasoning_content) so a
	// thinking-mode assistant turn can be replayed within the same run —
	// thinking providers (deepseek et al.) reject the follow-up request
	// without it.
	Reasoning  string     `json:"reasoning_content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// ProviderState is opaque, provider-specific reasoning that must be sent
	// back with the assistant turn for the next request in the same run
	// (Anthropic thinking blocks with signatures, Responses reasoning items).
	// It is never persisted across runs and never reaches chat-completions
	// providers.
	ProviderState json.RawMessage `json:"-"`
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
		Role       string         `json:"role"`
		Content    string         `json:"content,omitempty"`
		Reasoning  string         `json:"reasoning_content,omitempty"`
		ToolCalls  []toolCallWire `json:"tool_calls,omitempty"`
		ToolCallID string         `json:"tool_call_id,omitempty"`
	}
	out := alias{Role: m.Role, Content: m.Content, Reasoning: m.Reasoning, ToolCallID: m.ToolCallID}
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

// ProviderStatusError carries the upstream HTTP status so the agent loop can
// map failures to actionable error codes (auth, rate limit, timeout) instead
// of collapsing every provider failure into ai.model_unavailable.
type ProviderStatusError struct {
	StatusCode int
	Body       string
}

func (e *ProviderStatusError) Error() string {
	if e == nil {
		return "provider error"
	}
	return fmt.Sprintf("model returned status %d: %s", e.StatusCode, e.Body)
}

// responseHeaderTimeout bounds the wait for the provider to start answering.
// The whole-request http.Client.Timeout is deliberately not used: it also
// covers reading the stream body, so it cut off any generation longer than the
// timeout. The run deadline (AI_AGENT_RUN_TIMEOUT_SECONDS) bounds total time.
const responseHeaderTimeout = 90 * time.Second

// DefaultMaxCompletionTokens caps one model turn when the provider would
// otherwise pick its own limit (often tiny, sometimes unbounded and costly).
const DefaultMaxCompletionTokens = 8192

func newDefaultHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
	}}
}

// Provider abstracts the streaming chat backend so the handler can later
// route between multiple sources (cloud, local vLLM/Ollama) without changes.
type Provider interface {
	StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (finishReason string, usage Usage, err error)
}

// Probe performs a bounded upstream health check without creating a chat
// completion. It is used by readiness diagnostics and operator tooling.
type Prober interface {
	Probe(context.Context) error
}

var _ Backend = (*Client)(nil)

type Client struct {
	baseURL      string
	apiKey       string
	model        string
	providerType ProviderType
	effort       ReasoningEffort
	gatewayToken string
	http         *http.Client
}

func (c *Client) withEffort(effort ReasoningEffort) *Client {
	c.effort = effort
	return c
}

func (c *Client) ProviderType() ProviderType {
	if c == nil {
		return ProviderOpenAICompatible
	}
	return c.providerType
}

// ModelID and ProviderName expose only non-secret routing metadata for audit
// and analytics. API keys are intentionally never returned.
func (c *Client) ModelID() string {
	if c == nil {
		return ""
	}
	return c.model
}

func (c *Client) ProviderName() string {
	if c == nil {
		return ""
	}
	return hostName(c.baseURL)
}

func (c *Client) Validate() error {
	if c == nil {
		return fmt.Errorf("model client is not configured")
	}
	return validateEndpoint(c.baseURL, c.model)
}

func (c *Client) Probe(ctx context.Context) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	u := strings.TrimRight(c.baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c.applyHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider health probe returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// WithGatewayToken sets an AI Gateway credential sent as the
// cf-aig-authorization header, separate from the upstream provider key in
// Authorization (Cloudflare AI Gateway authentication mode).
func (c *Client) WithGatewayToken(token string) Backend {
	c.gatewayToken = strings.TrimSpace(token)
	return c
}

func (c *Client) applyHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	applyGatewayHeaders(req, c.providerType, c.gatewayToken)
}

// ChatProbe verifies credentials and reachability with a minimal
// chat-completions request. Unlike Probe (GET /models), it works with gateways
// that only expose the OpenAI-compatible chat endpoint and applies the same
// Authorization / cf-aig-authorization headers as real runs.
func (c *Client) ChatProbe(ctx context.Context) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 16,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("provider returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func NewClient(baseURL, apiKey, model string, httpClient *http.Client) *Client {
	return NewProviderClient(ProviderOpenAICompatible, baseURL, apiKey, model, httpClient)
}

func NewProviderClient(providerType ProviderType, baseURL, apiKey, model string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = newDefaultHTTPClient()
	}
	return &Client{
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:       apiKey,
		model:        model,
		providerType: providerType,
		http:         httpClient,
	}
}

type streamRequest struct {
	Model    string       `json:"model"`
	Messages []Message    `json:"messages"`
	Tools    []toolSchema `json:"tools,omitempty"`
	Stream   bool         `json:"stream"`
	// MaxTokens and MaxCompletionTokens are mutually exclusive: OpenAI's own
	// reasoning models reject max_tokens, while most compatible servers only
	// understand it.
	MaxTokens           int    `json:"max_tokens,omitempty"`
	MaxCompletionTokens int    `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string `json:"reasoning_effort,omitempty"`
	StreamOptions       *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

type toolSchema struct {
	Type     string         `json:"type"`
	Function toolDefinition `json:"function"`
}

type toolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type StreamCallbacks struct {
	OnTextDelta func(delta string)
	OnToolCall  func(call ToolCall)
	OnFinish    func(reason string, usage Usage)
	// OnReasoningDelta surfaces provider chain-of-thought deltas
	// (e.g. deepseek reasoning_content) for reasoning-aware clients.
	OnReasoningDelta func(delta string)
	// OnServedModel reports, once per stream, the exact model ID the provider
	// says answered. Aliases resolve to versioned IDs (jev-latest to
	// jev-1.13.0, claude-sonnet-5-5 to a dated release), so this is what an
	// audit trail should record.
	OnServedModel func(model string)
	// OnProviderState receives the opaque reasoning state of a finished turn
	// (see Message.ProviderState). Only formats that need replay call it.
	OnProviderState func(state json.RawMessage)
}

// StreamChat sends a chat completion request and consumes the SSE stream.
// It returns the finish reason of the last choice plus token usage when the
// provider reports it.
func (c *Client) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (string, Usage, error) {
	if err := c.Validate(); err != nil {
		return "", Usage{}, err
	}
	request := streamRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   true,
	}
	request.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}
	if c.providerType == ProviderOpenAI {
		request.MaxCompletionTokens = DefaultMaxCompletionTokens
	} else {
		request.MaxTokens = DefaultMaxCompletionTokens
	}
	request.ReasoningEffort = string(c.effort)
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
	response, err := c.doWithRetry(ctx, payload)
	if err != nil {
		return "", Usage{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return "", Usage{}, &ProviderStatusError{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	finishReason := ""
	var usage Usage
	pending := newPendingToolCalls()
	var think thinkFilter
	servedReported := false
	emitText := func(text, reasoning string) {
		if reasoning != "" && callbacks.OnReasoningDelta != nil {
			callbacks.OnReasoningDelta(reasoning)
		}
		if text != "" && callbacks.OnTextDelta != nil {
			callbacks.OnTextDelta(text)
		}
	}

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
		if chunk.Model != "" && !servedReported {
			servedReported = true
			if callbacks.OnServedModel != nil {
				callbacks.OnServedModel(chunk.Model)
			}
		}
		// Gateways report mid-stream failures as an `error` object in a data
		// frame. Skipping it would end the run as a "success" with a truncated
		// or empty answer.
		if hasStreamError(chunk.Error) {
			return finishReason, usage, &ProviderStatusError{StatusCode: http.StatusBadGateway, Body: truncateString(string(chunk.Error), 1024)}
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		for _, choice := range chunk.Choices {
			delta := choice.Delta
			if delta.Content != "" {
				emitText(think.feed(delta.Content))
			}
			if reasoning := delta.reasoningText(); reasoning != "" && callbacks.OnReasoningDelta != nil {
				callbacks.OnReasoningDelta(reasoning)
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
	emitText(think.flush())
	if callbacks.OnFinish != nil {
		callbacks.OnFinish(finishReason, usage)
	}
	return finishReason, usage, nil
}

const maxProviderAttempts = 3

func (c *Client) doWithRetry(ctx context.Context, payload []byte) (*http.Response, error) {
	return retrySend(ctx, c.http, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		c.applyHeaders(req)
		return req, nil
	})
}

const maxRetryAfter = 10 * time.Second

// parseRetryAfter understands both delay-seconds and HTTP-date forms.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

func retryableProviderStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout ||
		status == 529 // Anthropic "overloaded"
}

// streamDelta carries the reasoning under whichever field the provider uses:
// reasoning_content (DeepSeek, Kimi, GLM, Qwen, vLLM), reasoning (OpenRouter,
// newer vLLM, Ollama) or reasoning_details (OpenRouter's structured form).
type streamDelta struct {
	Content          string         `json:"content"`
	Reasoning        string         `json:"reasoning_content"`
	ReasoningAlias   string         `json:"reasoning"`
	ReasoningDetails []reasoningRef `json:"reasoning_details"`
	ToolCalls        []toolCallWire `json:"tool_calls"`
}

type reasoningRef struct {
	Text    string `json:"text"`
	Summary string `json:"summary"`
}

func (d streamDelta) reasoningText() string {
	if d.Reasoning != "" {
		return d.Reasoning
	}
	if d.ReasoningAlias != "" {
		return d.ReasoningAlias
	}
	var b strings.Builder
	for _, ref := range d.ReasoningDetails {
		b.WriteString(ref.Text)
		b.WriteString(ref.Summary)
	}
	return b.String()
}

type streamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta        streamDelta `json:"delta"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage          `json:"usage"`
	Error json.RawMessage `json:"error,omitempty"`
}

func hasStreamError(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null" && trimmed != "{}" && trimmed != `""`
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
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
