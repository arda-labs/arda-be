package model

import (
	"context"
	"errors"
	"sync"
)

// ChainProvider attempts the next provider only when the previous provider
// fails before emitting any output. Retrying after a partial stream would
// duplicate text or tool calls in the client transcript.
type ChainProvider struct {
	providers []Provider
	mu        sync.RWMutex
	selected  int
}

func NewChainProvider(providers ...Provider) Provider {
	items := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if provider != nil {
			items = append(items, provider)
		}
	}
	if len(items) == 0 {
		return nil
	}
	if len(items) == 1 {
		return items[0]
	}
	return &ChainProvider{providers: items}
}

func (p *ChainProvider) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (string, Usage, error) {
	var lastErr error
	for i, provider := range p.providers {
		emitted := false
		wrapped := callbacks
		wrapped.OnTextDelta = func(delta string) {
			emitted = true
			if callbacks.OnTextDelta != nil {
				callbacks.OnTextDelta(delta)
			}
		}
		wrapped.OnReasoningDelta = func(delta string) {
			emitted = true
			if callbacks.OnReasoningDelta != nil {
				callbacks.OnReasoningDelta(delta)
			}
		}
		wrapped.OnToolCall = func(call ToolCall) {
			emitted = true
			if callbacks.OnToolCall != nil {
				callbacks.OnToolCall(call)
			}
		}
		reason, usage, err := provider.StreamChat(ctx, messages, tools, wrapped)
		if err == nil {
			p.mu.Lock()
			p.selected = i
			p.mu.Unlock()
			return reason, usage, nil
		}
		lastErr = err
		if emitted || ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no model provider configured")
	}
	return "", Usage{}, lastErr
}

// A chain intentionally reports an aggregate descriptor. Persisting the first
// provider as if it were always selected would make cost/audit data incorrect.
func (p *ChainProvider) selectedProvider() Provider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.providers) == 0 {
		return nil
	}
	if p.selected < 0 || p.selected >= len(p.providers) {
		return p.providers[0]
	}
	return p.providers[p.selected]
}
func (p *ChainProvider) ProviderName() string {
	if d, ok := p.selectedProvider().(interface{ ProviderName() string }); ok {
		return d.ProviderName()
	}
	return "unknown"
}
func (p *ChainProvider) ModelID() string {
	if d, ok := p.selectedProvider().(interface{ ModelID() string }); ok {
		return d.ModelID()
	}
	return ""
}
