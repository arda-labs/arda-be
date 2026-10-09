package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// maxListedModels bounds what one /models response may contribute.
const maxListedModels = 500

// fetchModelIDs GETs a provider model list and returns the chat-capable IDs,
// sorted. It understands the OpenAI/Anthropic shape ({"data":[{"id"}]}) and
// the Gemini shape ({"models":[{"name":"models/x"}]}). Decision (System One)
// models are dropped: chat profiles refuse them.
func fetchModelIDs(ctx context.Context, client *http.Client, url string, apply func(*http.Request)) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	apply(req)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("provider returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name    string   `json:"name"`
			Methods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode model list: %w", err)
	}
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimPrefix(strings.TrimSpace(id), "models/")
		if id == "" || IsDecisionModelID(id) || len(seen) >= maxListedModels {
			return
		}
		seen[id] = struct{}{}
	}
	for _, item := range payload.Data {
		add(item.ID)
	}
	for _, item := range payload.Models {
		if len(item.Methods) > 0 && !containsString(item.Methods, "generateContent") {
			continue // embedding-only and similar models
		}
		add(item.Name)
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return fetchModelIDs(ctx, c.http, c.baseURL+"/models", c.applyHeaders)
}

func (c *responsesClient) ListModels(ctx context.Context) ([]string, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	return fetchModelIDs(ctx, c.http, c.baseURL+"/models", c.applyHeaders)
}

func (c *anthropicClient) ListModels(ctx context.Context) ([]string, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	return fetchModelIDs(ctx, c.http, endpointURL(c.baseURL, "/models", true)+"?limit=1000", c.applyHeaders)
}

// SuggestAPIFormat guesses the wire format a model ID is served on by gateways
// that expose several (OpenCode Zen routes Claude to /v1/messages, GPT-5.x and
// Grok to /responses, Gemini to /models/{id}, everything else to chat
// completions). It is a hint for the form, never applied silently: a first-party
// endpoint may offer a different protocol for the same model.
func SuggestAPIFormat(modelID string) APIFormat {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if slash := strings.LastIndex(id, "/"); slash >= 0 {
		id = id[slash+1:]
	}
	switch {
	case strings.HasPrefix(id, "claude-"):
		return FormatAnthropicMessages
	case strings.HasPrefix(id, "gemini-"):
		return FormatGoogleGemini
	case strings.HasPrefix(id, "gpt-5"), strings.HasPrefix(id, "gpt-6"),
		strings.HasPrefix(id, "grok-"), strings.HasPrefix(id, "muse-"),
		strings.HasPrefix(id, "o1"), strings.HasPrefix(id, "o3"), strings.HasPrefix(id, "o4"),
		strings.Contains(id, "codex"):
		return FormatOpenAIResponses
	default:
		return FormatChatCompletions
	}
}
