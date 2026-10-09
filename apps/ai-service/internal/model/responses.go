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

// responsesClient speaks the OpenAI Responses API (POST {base}/responses),
// statelessly (store=false): the full conversation is sent on every step and
// encrypted reasoning items are carried back through Message.ProviderState.
type responsesClient struct {
	baseURL      string
	apiKey       string
	model        string
	providerType ProviderType
	effort       ReasoningEffort
	gatewayToken string
	http         *http.Client
}

var _ Backend = (*responsesClient)(nil)

func newResponsesClient(opts Options, baseURL, apiKey, modelID string, httpClient *http.Client) *responsesClient {
	if httpClient == nil {
		httpClient = newDefaultHTTPClient()
	}
	return &responsesClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:       apiKey,
		model:        modelID,
		providerType: opts.ProviderType,
		effort:       opts.ReasoningEffort,
		http:         httpClient,
	}
}

func (c *responsesClient) ModelID() string      { return c.model }
func (c *responsesClient) ProviderName() string { return hostName(c.baseURL) }

func (c *responsesClient) WithGatewayToken(token string) Backend {
	c.gatewayToken = strings.TrimSpace(token)
	return c
}

func (c *responsesClient) validate() error { return validateEndpoint(c.baseURL, c.model) }

func (c *responsesClient) applyHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	applyGatewayHeaders(req, c.providerType, c.gatewayToken)
}

func (c *responsesClient) Probe(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
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

func (c *responsesClient) ChatProbe(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	// 16 is the smallest max_output_tokens the API accepts.
	payload, err := jsonBody(map[string]any{
		"model": c.model, "input": "ping", "max_output_tokens": 16, "store": false,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(payload))
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

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type responsesReasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary"`
}

type responsesRequest struct {
	Model           string              `json:"model"`
	Instructions    string              `json:"instructions,omitempty"`
	Input           []any               `json:"input"`
	Tools           []responsesTool     `json:"tools,omitempty"`
	Stream          bool                `json:"stream"`
	Store           bool                `json:"store"`
	MaxOutputTokens int                 `json:"max_output_tokens"`
	Reasoning       *responsesReasoning `json:"reasoning,omitempty"`
	Include         []string            `json:"include,omitempty"`
	PromptCacheKey  string              `json:"prompt_cache_key,omitempty"`
}

// buildResponsesRequest converts the provider-neutral conversation. Leading
// system messages become instructions; later ones stay in the input as system
// items so their position relative to tool output is kept.
func buildResponsesRequest(model string, messages []Message, tools []ToolDef, effort ReasoningEffort, cacheKey string) responsesRequest {
	request := responsesRequest{
		Model: model, Stream: true, Store: false, MaxOutputTokens: DefaultMaxCompletionTokens, PromptCacheKey: cacheKey,
	}
	var instructions []string
	input := make([]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "system":
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			if len(input) == 0 {
				instructions = append(instructions, message.Content)
			} else {
				input = append(input, map[string]any{"role": "system", "content": message.Content})
			}
		case "user":
			if strings.TrimSpace(message.Content) != "" {
				input = append(input, map[string]any{"role": "user", "content": message.Content})
			}
		case "assistant":
			if len(message.ProviderState) > 0 {
				var items []json.RawMessage
				if json.Unmarshal(message.ProviderState, &items) == nil {
					for _, item := range items {
						input = append(input, item)
					}
				}
			}
			if strings.TrimSpace(message.Content) != "" {
				input = append(input, map[string]any{
					"role":    "assistant",
					"content": []map[string]any{{"type": "output_text", "text": message.Content}},
				})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type": "function_call", "call_id": call.ID, "name": call.Name,
					"arguments": string(objectArguments(call.Arguments)),
				})
			}
		case "tool":
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content,
			})
		}
	}
	request.Instructions = strings.Join(instructions, "\n\n")
	request.Input = input
	for _, tool := range tools {
		parameters := tool.Parameters
		if len(parameters) == 0 {
			parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		request.Tools = append(request.Tools, responsesTool{Type: "function", Name: tool.Name, Description: tool.Description, Parameters: parameters})
	}
	if effort != EffortDefault {
		request.Reasoning = &responsesReasoning{Effort: string(effort), Summary: "auto"}
		// Stateless reasoning replay needs the encrypted items back.
		request.Include = []string{"reasoning.encrypted_content"}
	}
	return request
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responsesEvent struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta"`
	Item     json.RawMessage `json:"item"`
	Message  string          `json:"message"`
	Error    json.RawMessage `json:"error"`
	Response *struct {
		Model             string          `json:"model"`
		Status            string          `json:"status"`
		Usage             *responsesUsage `json:"usage"`
		Error             json.RawMessage `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
}

func (c *responsesClient) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (string, Usage, error) {
	if err := c.validate(); err != nil {
		return "", Usage{}, err
	}
	payload, err := jsonBody(buildResponsesRequest(c.model, messages, tools, c.effort, sessionIDFromContext(ctx)))
	if err != nil {
		return "", Usage{}, fmt.Errorf("encode model request: %w", err)
	}
	response, err := retrySend(ctx, c.http, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(payload))
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
		usage        Usage
		finishReason = "stop"
		sawToolCall  bool
		replay       []json.RawMessage
	)
	err = readSSE(response.Body, func(_ string, data []byte) error {
		var event responsesEvent
		if json.Unmarshal(data, &event) != nil {
			return nil
		}
		switch event.Type {
		case "response.created":
			if event.Response != nil && event.Response.Model != "" && callbacks.OnServedModel != nil {
				callbacks.OnServedModel(event.Response.Model)
			}
		case "response.output_text.delta":
			if event.Delta != "" && callbacks.OnTextDelta != nil {
				callbacks.OnTextDelta(event.Delta)
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if event.Delta != "" && callbacks.OnReasoningDelta != nil {
				callbacks.OnReasoningDelta(event.Delta)
			}
		case "response.output_item.done":
			var item responsesItem
			if json.Unmarshal(event.Item, &item) != nil {
				return nil
			}
			switch item.Type {
			case "function_call":
				sawToolCall = true
				if callbacks.OnToolCall != nil {
					callbacks.OnToolCall(ToolCall{ID: item.CallID, Name: item.Name, Arguments: string(objectArguments(item.Arguments))})
				}
			case "reasoning":
				replay = append(replay, event.Item)
			}
		case "response.completed", "response.incomplete":
			if event.Response != nil {
				if event.Response.Usage != nil {
					usage = Usage{
						PromptTokens: event.Response.Usage.InputTokens, CompletionTokens: event.Response.Usage.OutputTokens,
						TotalTokens: event.Response.Usage.TotalTokens,
					}
					if usage.TotalTokens == 0 {
						usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
					}
				}
				if event.Response.IncompleteDetails != nil && event.Response.IncompleteDetails.Reason == "max_output_tokens" {
					finishReason = "length"
				}
			}
			return errStreamDone
		case "response.failed":
			if event.Response != nil && len(event.Response.Error) > 0 {
				return providerStreamError(event.Response.Error)
			}
			return providerStreamError(json.RawMessage(`"response failed"`))
		case "error":
			raw := event.Error
			if len(raw) == 0 {
				raw, _ = json.Marshal(event.Message)
			}
			return providerStreamError(raw)
		}
		return nil
	})
	if err != nil && err != errStreamDone {
		return finishReason, usage, err
	}
	if sawToolCall && finishReason == "stop" {
		finishReason = "tool_calls"
	}
	if len(replay) > 0 && callbacks.OnProviderState != nil {
		if encoded, marshalErr := json.Marshal(replay); marshalErr == nil {
			callbacks.OnProviderState(encoded)
		}
	}
	if callbacks.OnFinish != nil {
		callbacks.OnFinish(finishReason, usage)
	}
	return finishReason, usage, nil
}
