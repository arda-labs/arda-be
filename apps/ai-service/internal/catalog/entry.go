package catalog

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/sandbox"
	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// CatalogEntry represents a single discoverable SDK method on the arda.* namespace.
type CatalogEntry struct {
	MethodName          string   // e.g. "crm.getCustomer"
	SDKPath             string   // e.g. "arda.crm.getCustomer"
	Domain              string   // e.g. "crm", "hrm", "finance", "knowledge"
	Service             string   // owning service for generated entries, e.g. "crm-service"; empty for local builtins
	Signature           string   // TypeScript function signature
	JSDoc               string   // Description, @param, @returns, @requires
	Keywords            []string // Indexed terms for BM25/keyword search
	Kind                string   // "read" | "confirm"
	RequiredPermissions []string // e.g. ["crm.customer.read"]
	Risk                string   // "low" | "medium" | "high"
	Timeout             time.Duration
	// Enabled is the contract-level default from the contract JSON `enabled`
	// field (absent = true). It is a hard floor: the runtime override in
	// ai_tool_settings can only disable further, never enable beyond it
	// (ADR-003). New entries must set it explicitly — see
	// TestBuiltinCatalogEntriesAreEnabled.
	Enabled bool
}

// DispatcherFunc is called when a script in the sandbox executes an arda.* method.
type DispatcherFunc func(ctx context.Context, scope tools.Context, args map[string]any) (any, error)

// DispatcherRegistry maps method names to their Go dispatcher execution functions.
type DispatcherRegistry struct {
	mu          sync.RWMutex
	dispatchers map[string]DispatcherFunc
	entries     map[string]CatalogEntry
	// enabled is the governance predicate (ADR-003). Nil means "use the
	// contract default on each entry"; a non-nil predicate evaluates
	// effectiveEnabled. Set once at startup via SetEnabledPredicate.
	enabled func(CatalogEntry) bool
}

func NewDispatcherRegistry() *DispatcherRegistry {
	return &DispatcherRegistry{
		dispatchers: make(map[string]DispatcherFunc),
		entries:     make(map[string]CatalogEntry),
	}
}

// SetEnabledPredicate wires the tool-governance predicate into the registry so
// execution and model-visible surfaces respect effectiveEnabled.
func (r *DispatcherRegistry) SetEnabledPredicate(fn func(CatalogEntry) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = fn
}

func (r *DispatcherRegistry) Register(entry CatalogEntry, fn DispatcherFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[entry.MethodName] = entry
	r.dispatchers[entry.MethodName] = fn
}

// snapshot copies the registries and the predicate under one read lock so the
// governance predicate is never invoked while the registry lock is held.
func (r *DispatcherRegistry) snapshot() (map[string]CatalogEntry, map[string]DispatcherFunc, func(CatalogEntry) bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make(map[string]CatalogEntry, len(r.entries))
	for name, entry := range r.entries {
		entries[name] = entry
	}
	dispatchers := make(map[string]DispatcherFunc, len(r.dispatchers))
	for name, fn := range r.dispatchers {
		dispatchers[name] = fn
	}
	return entries, dispatchers, r.enabled
}

func entryEnabled(fn func(CatalogEntry) bool, entry CatalogEntry) bool {
	if fn == nil {
		return entry.Enabled
	}
	return fn(entry)
}

// Resolve returns the dispatcher for a method. A method disabled by governance
// is reported as not resolvable (fail closed) so the approval-execution path
// cannot run a revoked tool.
func (r *DispatcherRegistry) Resolve(methodName string) (DispatcherFunc, CatalogEntry, bool) {
	entries, dispatchers, enabled := r.snapshot()
	fn, ok := dispatchers[methodName]
	if !ok {
		return nil, CatalogEntry{}, false
	}
	entry, exists := entries[methodName]
	if !exists {
		return fn, CatalogEntry{}, false
	}
	if !entryEnabled(enabled, entry) {
		return nil, entry, false
	}
	return fn, entry, true
}

// AllEntries returns every registered entry, including tools disabled by
// governance: the admin inventory and audit surfaces need to see them.
func (r *DispatcherRegistry) AllEntries() []CatalogEntry {
	entries, _, _ := r.snapshot()
	items := make([]CatalogEntry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, entry)
	}
	return items
}

// EnabledEntries returns only entries whose effectiveEnabled is true: the
// model-visible catalog (TypeDefs, capability listing, proposals).
func (r *DispatcherRegistry) EnabledEntries() []CatalogEntry {
	entries, _, enabled := r.snapshot()
	items := make([]CatalogEntry, 0, len(entries))
	for _, entry := range entries {
		if entryEnabled(enabled, entry) {
			items = append(items, entry)
		}
	}
	return items
}

// AllSDKMethods returns the sandbox SDK surface. Disabled methods are not
// injected into the sandbox at all, so a run cannot reach them.
func (r *DispatcherRegistry) AllSDKMethods() []sandbox.SDKMethod {
	entries, dispatchers, enabled := r.snapshot()
	items := make([]sandbox.SDKMethod, 0, len(entries))
	for name, entry := range entries {
		if !entryEnabled(enabled, entry) {
			continue
		}
		fn := dispatchers[name]
		entryCopy := entry
		items = append(items, sandbox.SDKMethod{
			MethodName:       entry.MethodName,
			SDKPath:          entry.SDKPath,
			Domain:           entry.Domain,
			Timeout:          entry.Timeout,
			RequiresApproval: entry.Kind == "confirm",
			Risk:             entry.Risk,
			CheckPermissions: entryCopy.CheckPermissions,
			Dispatcher:       fn,
		})
	}
	return items
}

// CheckPermissions verifies if scope has all required permissions for this entry.
func (e *CatalogEntry) CheckPermissions(scope tools.Context) error {
	if strings.TrimSpace(scope.TenantID) == "" || strings.TrimSpace(scope.ActorUserID) == "" {
		return tools.ErrToolForbidden
	}
	if _, superadmin := scope.Permissions["superadmin"]; superadmin {
		return nil
	}
	for _, perm := range e.RequiredPermissions {
		if _, allowed := scope.Permissions[perm]; !allowed {
			return fmt.Errorf("%w: missing permission %s", tools.ErrToolForbidden, perm)
		}
	}
	return nil
}
