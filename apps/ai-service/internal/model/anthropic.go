package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const anthropicVersion = "2023-06-01"

// anthropicClient speaks the Anthropic Messages API (POST {base}/v1/messages).
type anthropicClient struct {
	baseURL      string
	apiKey       string
	model        string
	providerType ProviderType
	effort       ReasoningEffort
	budget       int
	gatewayToken string
	http         *http.Client
}

var _ Backend = (*anthropicClient)(nil)

func newAnthropicClient(opts Options, baseURL, apiKey, modelID string, httpClient *http.Client) *anthropicClient {
	if httpClient == nil {
		httpClient = newDefaultHTTPClient()
	}
	return &anthropicClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:       apiKey,
		model:        modelID,
		providerType: opts.ProviderType,
		effort:       opts.ReasoningEffort,
		budget:       opts.ReasoningBudget,
		http:         httpClient,
	}
}

func (c *anthropicClient) ModelID() string      { return c.model }
func (c *anthropicClient) ProviderName() string { return hostName(c.baseURL) }

func (c *anthropicClient) WithGatewayToken(token string) Backend {
	c.gatewayToken = strings.TrimSpace(token)
	return c
}

func (c *anthropicClient) validate() error { return validateEndpoint(c.baseURL, c.model) }

func (c *anthropicClient) messagesURL() string { return endpointURL(c.baseURL, "/messages", true) }

func (c *anthropicClient) applyHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}
	req.Header.Set("anthropic-version", anthropicVersion)
	applyGatewayHeaders(req, c.providerType, c.gatewayToken)
}

func (c *anthropicClient) Probe(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL(c.baseURL, "/models", true), nil)
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

func (c *anthropicClient) ChatProbe(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	payload, err := jsonBody(map[string]any{
		"model":      c.model,
		"max_tokens": 16,
		"messages":   []map[string]any{{"role": "user", "content": "ping"}},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.messagesURL(), bytes.NewReader(payload))
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

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	System    []anthropicSystemBlock `json:"system,omitempty"`
	Messages  []anthropicMessage     `json:"messages"`
	Tools     []anthropicTool        `json:"tools,omitempty"`
	Thinking  *anthropicThinking     `json:"thinking,omitempty"`
	Stream    bool                   `json:"stream"`
}

// buildAnthropicRequest converts the provider-neutral conversation. Anthropic
// has no system role inside the conversation: leading system messages become
// the top-level system, later ones (such as the final-step instruction) become
// text in the next user turn. Tool results travel as tool_result blocks in a
// user turn, and adjacent same-role content is merged.
func buildAnthropicRequest(model string, messages []Message, tools []ToolDef, effort ReasoningEffort, explicitBudget int) anthropicRequest {
	request := anthropicRequest{Model: model, MaxTokens: DefaultMaxCompletionTokens, Stream: true}
	var conversation []anthropicMessage
	appendBlocks := func(role string, blocks ...any) {
		if len(blocks) == 0 {
			return
		}
		if n := len(conversation); n > 0 && conversation[n-1].Role == role {
			conversation[n-1].Content = append(conversation[n-1].Content, blocks...)
			return
		}
		conversation = append(conversation, anthropicMessage{Role: role, Content: blocks})
	}
	textBlock := func(text string) map[string]any { return map[string]any{"type": "text", "text": text} }

	for _, message := range messages {
		switch message.Role {
		case "system":
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			if len(conversation) == 0 {
				request.System = append(request.System, anthropicSystemBlock{Type: "text", Text: message.Content})
			} else {
				appendBlocks("user", textBlock(message.Content))
			}
		case "user":
			if strings.TrimSpace(message.Content) != "" {
				appendBlocks("user", textBlock(message.Content))
			}
		case "assistant":
			var blocks []any
			if len(message.ProviderState) > 0 {
				var replay []json.RawMessage
				if json.Unmarshal(message.ProviderState, &replay) == nil {
					for _, block := range replay {
						blocks = append(blocks, block)
					}
				}
			}
			if strings.TrimSpace(message.Content) != "" {
				blocks = append(blocks, textBlock(message.Content))
			}
			for _, call := range message.ToolCalls {
				blocks = append(blocks, map[string]any{
					"type": "tool_use", "id": call.ID, "name": call.Name, "input": objectArguments(call.Arguments),
				})
			}
			if len(blocks) > 0 {
				conversation = append(conversation, anthropicMessage{Role: "assistant", Content: blocks})
			}
		case "tool":
			appendBlocks("user", map[string]any{
				"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content,
			})
		}
	}
	request.Messages = conversation

	// Cache the system prefix: it carries the SDK type definitions and is
	// resent unchanged on every step of a run.
	if n := len(request.System); n > 0 {
		request.System[n-1].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
	}
	for _, tool := range tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		request.Tools = append(request.Tools, anthropicTool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	if budget := thinkingBudget(effort, explicitBudget); budget > 0 {
		request.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: budget}
		if request.MaxTokens < budget+4096 {
			request.MaxTokens = budget + 4096
		}
	}
	return request
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type anthropicEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Model string         `json:"model"`
		Usage anthropicUsage `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Data      string `json:"data"`
		Thinking  string `json:"thinking"`
		Signature string `json:"signature"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anthropicUsage `json:"usage"`
	Error json.RawMessage `json:"error"`
}

type anthropicBlock struct {
	kind      string
	id        string
	name      string
	thinking  strings.Builder
	signature string
	data      string
	arguments strings.Builder
}

func (c *anthropicClient) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (string, Usage, error) {
	if err := c.validate(); err != nil {
		return "", Usage{}, err
	}
	payload, err := jsonBody(buildAnthropicRequest(c.model, messages, tools, c.effort, c.budget))
	if err != nil {
		return "", Usage{}, fmt.Errorf("encode model request: %w", err)
	}
	response, err := retrySend(ctx, c.http, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.messagesURL(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		c.applyHeaders(req)
		return req, nil
	})
	if err != nil {
		return "", Usage{}, err
	}
	defer response.Body.Close()

	var (
		usage        anthropicUsage
		finishReason string
		blocks       = map[int]*anthropicBlock{}
		replay       []json.RawMessage
	)
	err = readSSE(response.Body, func(_ string, data []byte) error {
		var event anthropicEvent
		if json.Unmarshal(data, &event) != nil {
			return nil
		}
		switch event.Type {
		case "message_start":
			if event.Message != nil {
				usage = event.Message.Usage
				if event.Message.Model != "" && callbacks.OnServedModel != nil {
					callbacks.OnServedModel(event.Message.Model)
				}
			}
		case "content_block_start":
			if event.ContentBlock == nil {
				return nil
			}
			block := &anthropicBlock{
				kind: event.ContentBlock.Type, id: event.ContentBlock.ID, name: event.ContentBlock.Name,
				signature: event.ContentBlock.Signature, data: event.ContentBlock.Data,
			}
			block.thinking.WriteString(event.ContentBlock.Thinking)
			blocks[event.Index] = block
		case "content_block_delta":
			block := blocks[event.Index]
			if block == nil || event.Delta == nil {
				return nil
			}
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != "" && callbacks.OnTextDelta != nil {
					callbacks.OnTextDelta(event.Delta.Text)
				}
			case "thinking_delta":
				block.thinking.WriteString(event.Delta.Thinking)
				if event.Delta.Thinking != "" && callbacks.OnReasoningDelta != nil {
					callbacks.OnReasoningDelta(event.Delta.Thinking)
				}
			case "signature_delta":
				block.signature += event.Delta.Signature
			case "input_json_delta":
				block.arguments.WriteString(event.Delta.PartialJSON)
			}
		case "content_block_stop":
			block := blocks[event.Index]
			if block == nil {
				return nil
			}
			switch block.kind {
			case "tool_use":
				if callbacks.OnToolCall != nil {
					callbacks.OnToolCall(ToolCall{ID: block.id, Name: block.name, Arguments: string(objectArguments(block.arguments.String()))})
				}
			case "thinking":
				encoded, _ := jsonBody(map[string]any{"type": "thinking", "thinking": block.thinking.String(), "signature": block.signature})
				replay = append(replay, encoded)
			case "redacted_thinking":
				encoded, _ := jsonBody(map[string]any{"type": "redacted_thinking", "data": block.data})
				replay = append(replay, encoded)
			}
			delete(blocks, event.Index)
		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != "" {
				finishReason = mapAnthropicStop(event.Delta.StopReason)
			}
			if event.Usage != nil {
				usage.OutputTokens = event.Usage.OutputTokens
			}
		case "error":
			return providerStreamError(event.Error)
		case "message_stop":
			return errStreamDone
		}
		return nil
	})
	if err != nil && err != errStreamDone {
		return finishReason, toUsage(usage), err
	}
	if len(replay) > 0 && callbacks.OnProviderState != nil {
		if encoded, marshalErr := json.Marshal(replay); marshalErr == nil {
			callbacks.OnProviderState(encoded)
		}
	}
	total := toUsage(usage)
	if callbacks.OnFinish != nil {
		callbacks.OnFinish(finishReason, total)
	}
	return finishReason, total, nil
}

func toUsage(u anthropicUsage) Usage {
	prompt := u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	return Usage{PromptTokens: prompt, CompletionTokens: u.OutputTokens, TotalTokens: prompt + u.OutputTokens}
}

// mapAnthropicStop normalizes stop_reason to the chat-completions vocabulary
// the agent loop understands.
func mapAnthropicStop(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return reason
	}
}
