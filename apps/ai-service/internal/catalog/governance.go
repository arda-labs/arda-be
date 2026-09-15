package catalog

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/repository"
)

// GovernanceCacheTTL bounds how long an override written on another replica
// can go unnoticed by this one. A write on this replica refreshes the snapshot
// immediately; other replicas converge within the TTL.
const GovernanceCacheTTL = 10 * time.Second

// ErrGovernanceUnavailable is returned when a governance write is attempted
// without a persistent ToolSettingsStore (dev/test without a database).
var ErrGovernanceUnavailable = errors.New("tool governance store unavailable")

// Governance owns the platform-level runtime overrides from ai_tool_settings
// (ADR-003). The effective state is
//
//	effectiveEnabled = contractEnabled && (overrideEnabled ?? true)
//
// so the contract default is a hard floor and an override can only restrict.
// The override snapshot is cached in memory because the search index and the
// sandbox execution path consult it synchronously; call EnsureFresh at request
// entry points (agent run, tools list, tool update, proposal creation).
type Governance struct {
	store repository.ToolSettingsStore
	ttl   time.Duration

	mu        sync.RWMutex
	overrides map[string]bool
	loadedAt  time.Time
	loaded    bool
}

func NewGovernance(store repository.ToolSettingsStore) *Governance {
	return &Governance{store: store, ttl: GovernanceCacheTTL, overrides: map[string]bool{}}
}

// SetTTL overrides the refresh window (test seam).
func (g *Governance) SetTTL(ttl time.Duration) {
	if g == nil || ttl <= 0 {
		return
	}
	g.mu.Lock()
	g.ttl = ttl
	g.mu.Unlock()
}

// Available reports whether governance writes can be persisted.
func (g *Governance) Available() bool {
	return g != nil && g.store != nil
}

// EnsureFresh reloads the override snapshot when the TTL has elapsed. On a
// store error the previous snapshot is kept (a stale kill switch beats no kill
// switch) and the error is returned for the caller to log.
func (g *Governance) EnsureFresh(ctx context.Context) error {
	if g == nil || g.store == nil {
		return nil
	}
	g.mu.RLock()
	fresh := g.loaded && time.Since(g.loadedAt) < g.ttl
	g.mu.RUnlock()
	if fresh {
		return nil
	}

	items, err := g.store.ListToolSettings(ctx)
	if err != nil {
		slog.Warn("ai tool governance: override refresh failed; keeping previous snapshot", "err", err)
		return err
	}
	next := make(map[string]bool, len(items))
	for _, item := range items {
		next[item.MethodName] = item.Enabled
	}
	g.mu.Lock()
	g.overrides = next
	g.loadedAt = time.Now()
	g.loaded = true
	g.mu.Unlock()
	return nil
}

// Snapshot returns a copy of the current override map (methodName → enabled).
// A missing key means "no override"; callers must not mutate the result.
func (g *Governance) Snapshot() map[string]bool {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make(map[string]bool, len(g.overrides))
	for method, enabled := range g.overrides {
		out[method] = enabled
	}
	return out
}

// Override returns the stored override for one method and whether it exists.
func (g *Governance) Override(methodName string) (bool, bool) {
	if g == nil {
		return false, false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	value, ok := g.overrides[methodName]
	return value, ok
}

// IsEnabled evaluates effectiveEnabled for one entry. It is safe to call
// without a context (the search index and registry predicates use it that
// way); call EnsureFresh at request entry points to bound staleness.
func (g *Governance) IsEnabled(entry CatalogEntry) bool {
	if !entry.Enabled {
		return false
	}
	if g == nil {
		return true
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	override, ok := g.overrides[entry.MethodName]
	return !ok || override
}

// SetOverride upserts the runtime override for one method and returns the
// stored updated_at. Callers validate that the method exists and that the
// contract allows it before calling.
func (g *Governance) SetOverride(ctx context.Context, methodName string, enabled bool, actor string) (time.Time, error) {
	if !g.Available() {
		return time.Time{}, ErrGovernanceUnavailable
	}
	updatedAt, err := g.store.UpsertToolSetting(ctx, methodName, enabled, actor)
	if err != nil {
		return time.Time{}, err
	}
	g.mu.Lock()
	if g.overrides == nil {
		g.overrides = map[string]bool{}
	}
	g.overrides[methodName] = enabled
	g.loadedAt = time.Now()
	g.loaded = true
	g.mu.Unlock()
	return updatedAt, nil
}

// ClearOverride deletes the override row so the method returns to the contract
// default immediately. The returned timestamp is the moment of the clear.
func (g *Governance) ClearOverride(ctx context.Context, methodName string, actor string) (time.Time, error) {
	if !g.Available() {
		return time.Time{}, ErrGovernanceUnavailable
	}
	if err := g.store.DeleteToolSetting(ctx, methodName); err != nil {
		return time.Time{}, err
	}
	g.mu.Lock()
	delete(g.overrides, methodName)
	g.loadedAt = time.Now()
	g.loaded = true
	g.mu.Unlock()
	return time.Now().UTC(), nil
}
