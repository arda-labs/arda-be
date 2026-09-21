package catalog

import (
	"context"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// Display enrichment is best-effort and bounded independently of its caller.
// Resolve + CheckPermissions preserve tool governance even for nested reads.
func displayRead(ctx context.Context, reg *DispatcherRegistry, scope tools.Context, method string, args map[string]any) (map[string]any, bool) {
	fn, entry, ok := reg.Resolve(method)
	if !ok || entry.Kind != "read" || entry.CheckPermissions(scope) != nil || ctx.Err() != nil {
		return nil, false
	}
	callCtx, cancel := context.WithTimeout(ctx, entry.Timeout)
	defer cancel()
	result, err := fn(callCtx, scope, args)
	if err != nil {
		return nil, false
	}
	data, ok := result.(map[string]any)
	return data, ok
}

func enrichIdentityDisplay(ctx context.Context, reg *DispatcherRegistry, scope tools.Context, me map[string]any) {
	ctx, cancel := context.WithTimeout(ctx, 4500*time.Millisecond)
	defer cancel()
	resolution := map[string]any{"tenant": "unavailable", "organizations": "unavailable"}
	me["displayResolution"] = resolution
	if data, ok := displayRead(ctx, reg, scope, "iam.getMyDisplayContext", nil); ok {
		tenant, _ := data["tenant"].(map[string]any)
		user, _ := data["user"].(map[string]any)
		// Refuse labels from a mismatched response, even for a global admin.
		if tenant["id"] == scope.TenantID && user["id"] == scope.ActorUserID {
			copyDisplayLabels(me["tenant"].(map[string]any), tenant)
			copyDisplayLabels(me["user"].(map[string]any), user)
			if name, _ := tenant["name"].(string); name != "" {
				resolution["tenant"] = "resolved"
			}
		}
	}

	details := make([]map[string]any, 0, len(scope.OrgIDs))
	pending := make(map[string]map[string]any, len(scope.OrgIDs))
	for _, id := range scope.OrgIDs {
		if _, exists := pending[id]; exists {
			continue
		}
		item := map[string]any{"id": id}
		details = append(details, item)
		pending[id] = item
	}
	me["organizationDetails"] = details
	if len(pending) == 0 {
		resolution["organizations"] = "resolved"
		return
	}
	// At most three pages of the existing tenant-scoped directory. A partial
	// lookup is explicit, and only exact membership IDs survive into me().
	for page := 1; page <= 3 && len(pending) > 0; page++ {
		data, ok := displayRead(ctx, reg, scope, "platform.listOrganizations", map[string]any{"page": float64(page), "limit": float64(20)})
		if !ok {
			break
		}
		items, _ := data["items"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			id, _ := item["id"].(string)
			if target, found := pending[id]; found {
				copyDisplayLabels(target, item)
				if name, _ := item["name"].(string); name != "" {
					delete(pending, id)
				}
			}
		}
		if len(items) < 20 {
			break
		}
	}
	if len(pending) == 0 {
		resolution["organizations"] = "resolved"
	} else if len(pending) < len(details) {
		resolution["organizations"] = "partial"
	}
}

func copyDisplayLabels(dst, src map[string]any) {
	for _, key := range []string{"name", "code"} {
		if value, ok := src[key].(string); ok && value != "" {
			dst[key] = value
		}
	}
}
