package policy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/casbin/casbin/v3/model"
	"github.com/redis/go-redis/v9"
)

// Watcher listens for policy change notifications via Redis Pub/Sub.
type Watcher struct {
	client   *redis.Client
	channel  string
	callback func() error
	stopCh   chan struct{}
}

// NewWatcher creates a Redis-backed policy watcher.
// Call Start() to begin listening. Pass nil client for in-memory mode (no sync).
func NewWatcher(client *redis.Client, channel string, onReload func() error) *Watcher {
	return &Watcher{
		client:   client,
		channel:  channel,
		callback: onReload,
		stopCh:   make(chan struct{}),
	}
}

// Start subscribes to the Redis channel and reloads policies on change.
func (w *Watcher) Start(ctx context.Context) {
	if w.client == nil {
		slog.Info("policy watcher: in-memory mode (no Redis)")
		return
	}

	pubsub := w.client.Subscribe(ctx, w.channel)
	_, err := pubsub.Receive(ctx)
	if err != nil {
		slog.Warn("policy watcher: subscribe failed", "err", err)
		return
	}

	ch := pubsub.Channel()
	slog.Info("policy watcher started", "channel", w.channel)

	go func() {
		for {
			select {
			case msg := <-ch:
				slog.Info("policy watcher: reload triggered", "msg", msg.Payload)
				if w.callback != nil {
					if err := w.callback(); err != nil {
						slog.Error("policy watcher: reload failed", "err", err)
					}
				}
			case <-w.stopCh:
				pubsub.Close()
				return
			}
		}
	}()
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	close(w.stopCh)
}

// Notify publishes a reload event to all instances.
func (w *Watcher) Notify(ctx context.Context) error {
	if w.client == nil {
		return nil
	}
	return w.client.Publish(ctx, w.channel, "reload").Err()
}

// ── In-memory adapter for development ──

// MemoryAdapter stores policies in-memory (no persistence).
type MemoryAdapter struct {
	policies []string
}

func NewMemoryAdapter() *MemoryAdapter {
	return &MemoryAdapter{}
}

func (a *MemoryAdapter) LoadPolicy(model model.Model) error {
	// In-memory — policies can be added via AddPolicy before LoadPolicy
	return nil
}

func (a *MemoryAdapter) SavePolicy(model model.Model) error {
	return nil
}

func (a *MemoryAdapter) AddPolicy(sec, ptype string, rule []string) error {
	a.policies = append(a.policies, fmt.Sprintf("%s, %s: %v", sec, ptype, rule))
	return nil
}

func (a *MemoryAdapter) RemovePolicy(sec, ptype string, rule []string) error {
	return nil
}

func (a *MemoryAdapter) RemoveFilteredPolicy(sec, ptype string, fieldIndex int, fieldValues ...string) error {
	return nil
}
