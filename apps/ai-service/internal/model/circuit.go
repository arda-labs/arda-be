package model

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("model provider circuit is open")

// CircuitBreakerProvider suppresses repeated calls to a failing upstream.
// Failures after output are excluded because the caller cannot safely retry a
// partially emitted response.
type CircuitBreakerProvider struct {
	provider  Provider
	mu        sync.Mutex
	failures  int
	threshold int
	openUntil time.Time
	cooldown  time.Duration
}

func (c *CircuitBreakerProvider) Probe(ctx context.Context) error {
	if p, ok := c.provider.(Prober); ok {
		return p.Probe(ctx)
	}
	return errors.New("provider does not support health probes")
}

func NewCircuitBreakerProvider(provider Provider, threshold int, cooldown time.Duration) Provider {
	if provider == nil {
		return nil
	}
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &CircuitBreakerProvider{provider: provider, threshold: threshold, cooldown: cooldown}
}

func (c *CircuitBreakerProvider) StreamChat(ctx context.Context, messages []Message, tools []ToolDef, callbacks StreamCallbacks) (string, Usage, error) {
	c.mu.Lock()
	if time.Now().Before(c.openUntil) {
		c.mu.Unlock()
		return "", Usage{}, ErrCircuitOpen
	}
	c.mu.Unlock()
	emitted := false
	wrapped := callbacks
	wrapped.OnTextDelta = func(s string) {
		emitted = true
		if callbacks.OnTextDelta != nil {
			callbacks.OnTextDelta(s)
		}
	}
	wrapped.OnReasoningDelta = func(s string) {
		emitted = true
		if callbacks.OnReasoningDelta != nil {
			callbacks.OnReasoningDelta(s)
		}
	}
	wrapped.OnToolCall = func(call ToolCall) {
		emitted = true
		if callbacks.OnToolCall != nil {
			callbacks.OnToolCall(call)
		}
	}
	reason, usage, err := c.provider.StreamChat(ctx, messages, tools, wrapped)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.failures = 0
		c.openUntil = time.Time{}
		return reason, usage, nil
	}
	if !emitted && ctx.Err() == nil {
		c.failures++
		if c.failures >= c.threshold {
			c.openUntil = time.Now().Add(c.cooldown)
		}
	}
	return reason, usage, err
}

func (c *CircuitBreakerProvider) ProviderName() string {
	if d, ok := c.provider.(interface{ ProviderName() string }); ok {
		return d.ProviderName()
	}
	return "unknown"
}
func (c *CircuitBreakerProvider) ModelID() string {
	if d, ok := c.provider.(interface{ ModelID() string }); ok {
		return d.ModelID()
	}
	return ""
}
