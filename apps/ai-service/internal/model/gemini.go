package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// geminiClient speaks the Gemini generateContent API
// (POST {base}/models/{model}:streamGenerateContent?alt=sse), which Google
// serves natively and OpenCode Zen exposes at {base}/models/{model}.
type geminiClient struct {
	baseURL      string
	apiKey       string
	model        string
	providerType ProviderType
	effort       ReasoningEffort
	budget       int
	gatewayToken string
	http         *http.Client
}

var _ Backend = (*geminiClient)(nil)

func newGeminiClient(opts Options, baseURL, apiKey, modelID string, httpClient *http.Client) *geminiClient {
	if httpClient == nil {
		httpClient = newDefaultHTTPClient()
	}
	return &geminiClient{
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:       apiKey,
		model:        modelID,
		providerType: opts.ProviderType,
		effort:       opts.ReasoningEffort,
		budget:       opts.ReasoningBudget,
		http:         httpClient,
	}
}

func (c *geminiClient) ModelID() string      { return c.model }
func (c *geminiClient) ProviderName() string { return hostName(c.baseURL) }

func (c *geminiClient) WithGatewayToken(token string) Backend {
	c.gatewayToken = strings.TrimSpace(token)
	return c
}

func (c *geminiClient) validate() error { return validateEndpoint(c.baseURL, c.model) }

func (c *geminiClient) modelURL(method string) string {
	return c.baseURL + "/models/" + url.PathEscape(c.model) + ":" + method
}

func (c *geminiClient) applyHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("x-goog-api-key", c.apiKey)
	}
	applyGatewayHeaders(req, c.providerType, c.gatewayToken)
}

func (c *geminiClient) Probe(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	_, err := fetchModelIDs(ctx, c.http, c.baseURL+"/models?pageSize=1", c.applyHeaders)
	return err
}

func (c *geminiClient) ListModels(ctx context.Context) ([]string, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	return fetchModelIDs(ctx, c.http, c.baseURL+"/models?pageSize=1000", c.applyHeaders)
}

func (c *geminiClient) ChatProbe(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	payload, err := jsonBody(map[string]any{
		"contents":         []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "ping"}}}},
		"generationConfig": map[string]any{"maxOutputTokens": 16},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.modelURL("generateContent"), bytes.NewReader(payload))
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

type geminiContent struct {
	Role  string `json:"role"`
	Parts []any  `json:"parts"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	Contents          []geminiContent  `json:"contents"`
	Tools             []map[string]any `json:"tools,omitempty"`
	GenerationConfig  map[string]any   `json:"generationConfig"`
}

// geminiSchemaKeys are the JSON-schema keywords Gemini's function declarations
// accept; anything else (additionalProperties, $schema, …) is a 400.
var geminiSchemaKeys = map[string]bool{
	"type": true, "format": true, "description": true, "nullable": true, "enum": true,
	"properties": true, "required": true, "items": true, "minItems": true, "maxItems": true,
	"minimum": true, "maximum": true, "minLength": true, "maxLength": true, "pattern": true, "anyOf": true,
}

// geminiSchema reduces a JSON schema to the subset Gemini accepts. A type
// array such as ["string","null"] becomes a single type plus nullable.
func geminiSchema(value any) any {
	switch node := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range node {
			if !geminiSchemaKeys[key] {
				continue
			}
			switch key {
			case "properties":
				properties := map[string]any{}
				if children, ok := child.(map[string]any); ok {
					for name, property := range children {
						properties[name] = geminiSchema(property)
					}
				}
				out[key] = properties
			case "items":
				out[key] = geminiSchema(child)
			case "anyOf":
				if variants, ok := child.([]any); ok {
					cleaned := make([]any, 0, len(variants))
					for _, variant := range variants {
						cleaned = append(cleaned, geminiSchema(variant))
					}
					out[key] = cleaned
				}
			case "type":
				if list, ok := child.([]any); ok {
					for _, entry := range list {
						if name, _ := entry.(string); name == "null" {
							out["nullable"] = true
						} else if _, set := out["type"]; !set {
							out["type"] = entry
						}
					}
				} else {
					out[key] = child
				}
			default:
				out[key] = child
			}
		}
		return out
	default:
		return value
	}
}

func geminiParameters(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var schema any
	if json.Unmarshal(raw, &schema) != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return geminiSchema(schema)
}

// geminiResponseObject wraps a tool result for functionResponse, which must be
// a JSON object.
func geminiResponseObject(content string) any {
	var object map[string]any
	if json.Unmarshal([]byte(content), &object) == nil && object != nil {
		return object
	}
	return map[string]any{"result": content}
}

// buildGeminiRequest converts the provider-neutral conversation. Gemini has
// no tool-call IDs, so a tool result is matched to its call by function name,
// recovered from the assistant turn that issued the ID. Thought signatures on
// function calls must travel back unchanged: they are kept in ProviderState.
func buildGeminiRequest(messages []Message, tools []ToolDef, effort ReasoningEffort, explicitBudget int) geminiRequest {
	request := geminiRequest{GenerationConfig: map[string]any{"maxOutputTokens": DefaultMaxCompletionTokens}}
	var contents []geminiContent
	appendParts := func(role string, parts ...any) {
		if len(parts) == 0 {
			return
		}
		if n := len(contents); n > 0 && contents[n-1].Role == role {
			contents[n-1].Parts = append(contents[n-1].Parts, parts...)
			return
		}
		contents = append(contents, geminiContent{Role: role, Parts: parts})
	}
	names := map[string]string{}
	for _, message := range messages {
		switch message.Role {
		case "system":
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			if len(contents) == 0 {
				if request.SystemInstruction == nil {
					request.SystemInstruction = &geminiContent{Role: "user"}
				}
				request.SystemInstruction.Parts = append(request.SystemInstruction.Parts, map[string]any{"text": message.Content})
			} else {
				appendParts("user", map[string]any{"text": message.Content})
			}
		case "user":
			if strings.TrimSpace(message.Content) != "" {
				appendParts("user", map[string]any{"text": message.Content})
			}
		case "assistant":
			var parts []any
			if strings.TrimSpace(message.Content) != "" {
				parts = append(parts, map[string]any{"text": message.Content})
			}
			var replay []json.RawMessage
			if len(message.ProviderState) > 0 {
				_ = json.Unmarshal(message.ProviderState, &replay)
			}
			if len(replay) > 0 {
				for _, part := range replay {
					parts = append(parts, part)
				}
				for _, call := range message.ToolCalls {
					names[call.ID] = call.Name
				}
			} else {
				for _, call := range message.ToolCalls {
					names[call.ID] = call.Name
					parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": objectArguments(call.Arguments)}})
				}
			}
			if len(parts) > 0 {
				contents = append(contents, geminiContent{Role: "model", Parts: parts})
			}
		case "tool":
			name := names[message.ToolCallID]
			if name == "" {
				name = "tool"
			}
			appendParts("user", map[string]any{"functionResponse": map[string]any{"name": name, "response": geminiResponseObject(message.Content)}})
		}
	}
	request.Contents = contents

	if len(tools) > 0 {
		declarations := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			declarations = append(declarations, map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": geminiParameters(tool.Parameters),
			})
		}
		request.Tools = []map[string]any{{"functionDeclarations": declarations}}
	}
	if budget := thinkingBudget(effort, explicitBudget); budget > 0 {
		request.GenerationConfig["thinkingConfig"] = map[string]any{"includeThoughts": true, "thinkingBudget": budget}
		if max, _ := request.GenerationConfig["maxOutputTokens"].(int); max < budget+4096 {
			request.GenerationConfig["maxOutputTokens"] = budget + 4096
		}
	}
	return request
}

type geminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiPart struct {
	Text         string `json:"text"`
	Thought      bool   `json:"thought"`
	FunctionCall *struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"functionCall"`
}

type geminiChunk struct {
	Candidates []struct {
		Content struct {
			Parts []json.RawMessage `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	ModelVersion  string          `json:"modelVersion"`
	UsageMetadata *geminiUsage    `json:"usageMetadata"`
	Error         json.RawMessage `json:"error"`
}

func (c *geminiClient) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (string, Usage, error) {
	if err := c.validate(); err != nil {
		return "", Usage{}, err
	}
	payload, err := jsonBody(buildGeminiRequest(messages, tools, c.effort, c.budget))
	if err != nil {
		return "", Usage{}, fmt.Errorf("encode model request: %w", err)
	}
	response, err := retrySend(ctx, c.http, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.modelURL("streamGenerateContent")+"?alt=sse", bytes.NewReader(payload))
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
		usage          Usage
		finishReason   string
		callCount      int
		replay         []json.RawMessage
		servedReported bool
	)
	err = readSSE(response.Body, func(_ string, data []byte) error {
		var chunk geminiChunk
		if json.Unmarshal(data, &chunk) != nil {
			return nil
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return providerStreamError(chunk.Error)
		}
		if chunk.PromptFeedback != nil && chunk.PromptFeedback.BlockReason != "" {
			return providerStreamError(json.RawMessage(fmt.Sprintf("%q", "prompt blocked: "+chunk.PromptFeedback.BlockReason)))
		}
		if chunk.ModelVersion != "" && !servedReported {
			servedReported = true
			if callbacks.OnServedModel != nil {
				callbacks.OnServedModel(chunk.ModelVersion)
			}
		}
		if meta := chunk.UsageMetadata; meta != nil {
			usage = Usage{
				PromptTokens:     meta.PromptTokenCount,
				CompletionTokens: meta.CandidatesTokenCount + meta.ThoughtsTokenCount,
				TotalTokens:      meta.TotalTokenCount,
			}
			if usage.TotalTokens == 0 {
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
		}
		for _, candidate := range chunk.Candidates {
			for _, raw := range candidate.Content.Parts {
				var part geminiPart
				if json.Unmarshal(raw, &part) != nil {
					continue
				}
				switch {
				case part.FunctionCall != nil:
					callCount++
					// The raw part keeps thoughtSignature, required on replay.
					replay = append(replay, raw)
					if callbacks.OnToolCall != nil {
						callbacks.OnToolCall(ToolCall{
							ID:        fmt.Sprintf("call_%d", callCount),
							Name:      part.FunctionCall.Name,
							Arguments: string(objectArguments(string(part.FunctionCall.Args))),
						})
					}
				case part.Thought:
					if part.Text != "" && callbacks.OnReasoningDelta != nil {
						callbacks.OnReasoningDelta(part.Text)
					}
				case part.Text != "":
					if callbacks.OnTextDelta != nil {
						callbacks.OnTextDelta(part.Text)
					}
				}
			}
			if candidate.FinishReason != "" {
				finishReason = mapGeminiFinish(candidate.FinishReason)
			}
		}
		return nil
	})
	if err != nil {
		return finishReason, usage, err
	}
	if callCount > 0 && (finishReason == "" || finishReason == "stop") {
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

// mapGeminiFinish normalizes finishReason to the chat-completions vocabulary.
func mapGeminiFinish(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	default:
		return strings.ToLower(reason)
	}
}
