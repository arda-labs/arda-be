package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIFormat is the wire protocol a profile speaks. It is independent of the
// provider preset: one gateway (for example OpenCode Zen) can serve all three
// formats on different paths.
type APIFormat string

const (
	// FormatChatCompletions is POST {base}/chat/completions (the default).
	FormatChatCompletions APIFormat = "chat_completions"
	// FormatAnthropicMessages is POST {base}/v1/messages.
	FormatAnthropicMessages APIFormat = "anthropic_messages"
	// FormatOpenAIResponses is POST {base}/responses.
	FormatOpenAIResponses APIFormat = "openai_responses"
	// FormatGoogleGemini is POST {base}/models/{model}:streamGenerateContent.
	FormatGoogleGemini APIFormat = "google_gemini"
)

// NormalizeAPIFormat maps stored/user input to a format. Empty selects the
// default so profiles created before the column existed keep working.
func NormalizeAPIFormat(raw string) (APIFormat, bool) {
	switch APIFormat(strings.ToLower(strings.TrimSpace(raw))) {
	case "", FormatChatCompletions:
		return FormatChatCompletions, true
	case FormatAnthropicMessages:
		return FormatAnthropicMessages, true
	case FormatOpenAIResponses:
		return FormatOpenAIResponses, true
	case FormatGoogleGemini:
		return FormatGoogleGemini, true
	default:
		return "", false
	}
}

// ReasoningEffort asks a reasoning-capable model to think harder or lighter.
// Empty means "provider default": nothing is sent, which is the only safe
// value for models that reject reasoning parameters.
type ReasoningEffort string

const (
	EffortDefault ReasoningEffort = ""
	EffortLow     ReasoningEffort = "low"
	EffortMedium  ReasoningEffort = "medium"
	EffortHigh    ReasoningEffort = "high"
)

func NormalizeReasoningEffort(raw string) (ReasoningEffort, bool) {
	switch ReasoningEffort(strings.ToLower(strings.TrimSpace(raw))) {
	case EffortDefault:
		return EffortDefault, true
	case EffortLow:
		return EffortLow, true
	case EffortMedium:
		return EffortMedium, true
	case EffortHigh:
		return EffortHigh, true
	default:
		return "", false
	}
}

// Options select how a profile's model is called.
type Options struct {
	ProviderType    ProviderType
	APIFormat       APIFormat
	ReasoningEffort ReasoningEffort
	// ReasoningBudget is an explicit thinking-token budget. When set it wins
	// over the effort mapping for the formats that take a budget (Anthropic
	// messages, Gemini); the others ignore it.
	ReasoningBudget int
}

// Reasoning-budget bounds: Anthropic requires at least 1024 thinking tokens,
// and 64k is far beyond any sensible per-turn cost for this service.
const (
	MinReasoningBudget = 1024
	MaxReasoningBudget = 64000
)

// ValidReasoningBudget accepts 0 (use the effort mapping) or a bounded value.
func ValidReasoningBudget(budget int) bool {
	return budget == 0 || (budget >= MinReasoningBudget && budget <= MaxReasoningBudget)
}

// Backend is a model client for one profile+model, whatever its wire format.
type Backend interface {
	Provider
	Prober
	ChatProbe(ctx context.Context) error
	// ListModels returns the model IDs the endpoint advertises.
	ListModels(ctx context.Context) ([]string, error)
	ModelID() string
	ProviderName() string
	WithGatewayToken(token string) Backend
}

// NewBackend builds the client for the configured API format.
func NewBackend(opts Options, baseURL, apiKey, modelID string, httpClient *http.Client) Backend {
	if opts.ProviderType == "" {
		opts.ProviderType = ProviderOpenAICompatible
	}
	switch opts.APIFormat {
	case FormatAnthropicMessages:
		return newAnthropicClient(opts, baseURL, apiKey, modelID, httpClient)
	case FormatOpenAIResponses:
		return newResponsesClient(opts, baseURL, apiKey, modelID, httpClient)
	case FormatGoogleGemini:
		return newGeminiClient(opts, baseURL, apiKey, modelID, httpClient)
	default:
		return NewProviderClient(opts.ProviderType, baseURL, apiKey, modelID, httpClient).withEffort(opts.ReasoningEffort)
	}
}

// endpointURL appends path to base. Anthropic's route lives under /v1 while
// gateways usually already include it in the base URL, so the version segment
// is only added when missing.
func endpointURL(base, path string, versioned bool) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if versioned {
		if parsed, err := url.Parse(base); err == nil {
			trimmed := strings.TrimRight(parsed.Path, "/")
			if !strings.HasSuffix(trimmed, "/v1") {
				base += "/v1"
			}
		}
	}
	return base + path
}

func validateEndpoint(baseURL, modelID string) error {
	if baseURL == "" || modelID == "" {
		return fmt.Errorf("model client is not configured")
	}
	if IsDecisionModelID(modelID) {
		return fmt.Errorf("decision models cannot be used for chat generation")
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("model base URL must be an http or https URL")
	}
	return nil
}

func hostName(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return "unknown"
	}
	return u.Hostname()
}

// retrySend POSTs with bounded retries on 429/502/503/504, honoring
// Retry-After. build is called per attempt so each gets a fresh body.
func retrySend(ctx context.Context, client *http.Client, build func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= maxProviderAttempts; attempt++ {
		var retryAfter time.Duration
		req, err := build()
		if err != nil {
			return nil, fmt.Errorf("create model request: %w", err)
		}
		response, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = fmt.Errorf("model request failed: %w", err)
		} else if response.StatusCode == http.StatusOK {
			return response, nil
		} else {
			retryAfter = parseRetryAfter(response.Header.Get("Retry-After"))
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
			response.Body.Close()
			lastErr = &ProviderStatusError{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(body))}
			if !retryableProviderStatus(response.StatusCode) {
				return nil, lastErr
			}
		}
		if attempt < maxProviderAttempts {
			backoff := time.Duration(1<<(attempt-1)) * 250 * time.Millisecond
			if retryAfter > backoff {
				backoff = retryAfter
			}
			if backoff > maxRetryAfter {
				backoff = maxRetryAfter
			}
			half := backoff / 2
			backoff = half + time.Duration(rand.Int64N(int64(half)+1))
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, lastErr
}

// readSSE walks a server-sent-event stream, calling fn once per data frame
// with the frame's event name (empty when the server sends none). fn returning
// an error stops the walk.
func readSSE(body io.Reader, fn func(event string, data []byte) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	event := ""
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		switch {
		case line == "":
			event = ""
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if err := fn(event, []byte(data)); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read model stream: %w", err)
	}
	return nil
}

// errStreamDone ends readSSE early without reporting a failure.
var errStreamDone = fmt.Errorf("stream done")

func providerStreamError(raw json.RawMessage) error {
	return &ProviderStatusError{StatusCode: http.StatusBadGateway, Body: truncateString(string(raw), 1024)}
}

func jsonBody(payload any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// objectArguments returns arguments as a JSON object, substituting {} for an
// empty or invalid string: both Anthropic and Responses need a real object.
func objectArguments(arguments string) json.RawMessage {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" || trimmed[0] != '{' || !validJSON(trimmed) {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(trimmed)
}

// thinkingBudget resolves the thinking-token budget: an explicit value wins,
// otherwise the effort level maps to a fixed one. Zero means "do not enable
// thinking".
func thinkingBudget(effort ReasoningEffort, explicit int) int {
	if explicit > 0 {
		return explicit
	}
	return effortBudget(effort)
}

// effortBudget maps an effort level to a thinking-token budget.
func effortBudget(effort ReasoningEffort) int {
	switch effort {
	case EffortLow:
		return 2048
	case EffortMedium:
		return 8192
	case EffortHigh:
		return 16384
	default:
		return 0
	}
}

// applyGatewayHeaders adds the headers shared by every wire format: the AI
// Gateway credential and the per-preset session affinity metadata.
func applyGatewayHeaders(req *http.Request, providerType ProviderType, gatewayToken string) {
	if gatewayToken != "" {
		req.Header.Set("cf-aig-authorization", "Bearer "+gatewayToken)
	}
	if providerType == ProviderOpenCodeGo {
		req.Header.Set("User-Agent", "arda-ai-service/1.0")
		if sessionID := sessionIDFromContext(req.Context()); sessionID != "" {
			req.Header.Set("x-opencode-session", sessionID)
		}
	}
}
