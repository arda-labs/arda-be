package model

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// ProviderType describes a server-owned transport preset. All currently
// supported presets use the OpenAI chat-completions wire protocol; they differ
// only in endpoint conventions and required request metadata.
type ProviderType string

// Jev uses System One typed decisions, not chat-completions.
func IsDecisionModelID(id string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(id)), "/")
	return strings.HasPrefix(parts[len(parts)-1], "jev-")
}

const (
	ProviderOpenAI           ProviderType = "openai"
	ProviderOpenAICompatible ProviderType = "openai-compatible"
	ProviderOpenCodeGo       ProviderType = "opencode-go"
	ProviderOllama           ProviderType = "ollama"
	ProviderVLLM             ProviderType = "vllm"
)

func NormalizeProviderType(raw string) (ProviderType, bool) {
	switch ProviderType(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ProviderOpenAICompatible:
		return ProviderOpenAICompatible, true
	case ProviderOpenAI:
		return ProviderOpenAI, true
	case ProviderOpenCodeGo:
		return ProviderOpenCodeGo, true
	case ProviderOllama:
		return ProviderOllama, true
	case ProviderVLLM:
		return ProviderVLLM, true
	default:
		return "", false
	}
}

type sessionContextKey struct{}

// WithSessionID carries an opaque, stable conversation key to providers that
// require session affinity. It is request-scoped so pooled clients are safe
// for concurrent conversations.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil || strings.TrimSpace(sessionID) == "" || sessionIDFromContext(ctx) != "" {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey{}, strings.TrimSpace(sessionID))
}

func sessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(sessionContextKey{}).(string)
	return strings.TrimSpace(value)
}

// StableSessionID produces a non-reversible provider session value. The
// service-auth secret is deliberately used as an HMAC key so tenant and AG-UI
// thread identifiers never leave Arda as upstream metadata.
func StableSessionID(secret, tenantID, conversationID string) string {
	key := []byte(strings.TrimSpace(secret))
	if len(key) == 0 {
		// Development/test fallback still avoids exposing raw identifiers.
		key = []byte("arda-ai-session-v1")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(strings.TrimSpace(tenantID)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(conversationID)))
	return "arda-" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
