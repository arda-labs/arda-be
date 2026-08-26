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
	Signature           string   // TypeScript function signature
	JSDoc               string   // Description, @param, @returns, @requires
	Keywords            []string // Indexed terms for BM25/keyword search
	Kind                string   // "read" | "confirm"
	RequiredPermissions []string // e.g. ["crm.customer.read"]
	Risk                string   // "low" | "medium" | "high"
	Timeout             time.Duration
}

// DispatcherFunc is called when a script in the sandbox executes an arda.* method.
type DispatcherFunc func(ctx context.Context, scope tools.Context, args map[string]any) (any, error)

// DispatcherRegistry maps method names to their Go dispatcher execution functions.
type DispatcherRegistry struct {
	mu          sync.RWMutex
	dispatchers map[string]DispatcherFunc
	entries     map[string]CatalogEntry
}

func NewDispatcherRegistry() *DispatcherRegistry {
	return &DispatcherRegistry{
		dispatchers: make(map[string]DispatcherFunc),
		entries:     make(map[string]CatalogEntry),
	}
}

func (r *DispatcherRegistry) Register(entry CatalogEntry, fn DispatcherFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[entry.MethodName] = entry
	r.dispatchers[entry.MethodName] = fn
}

func (r *DispatcherRegistry) Resolve(methodName string) (DispatcherFunc, CatalogEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.dispatchers[methodName]
	if !ok {
		return nil, CatalogEntry{}, false
	}
	entry, exists := r.entries[methodName]
	return fn, entry, exists
}

func (r *DispatcherRegistry) AllEntries() []CatalogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]CatalogEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		items = append(items, entry)
	}
	return items
}

func (r *DispatcherRegistry) AllSDKMethods() []sandbox.SDKMethod {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]sandbox.SDKMethod, 0, len(r.entries))
	for name, entry := range r.entries {
		fn := r.dispatchers[name]
		entryCopy := entry
		items = append(items, sandbox.SDKMethod{
			MethodName:       entry.MethodName,
			SDKPath:          entry.SDKPath,
			Domain:           entry.Domain,
			Timeout:          entry.Timeout,
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
