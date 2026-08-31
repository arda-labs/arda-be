package catalog

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/tools"
)

// RegisterIAMCatalog registers IAM self-service SDK methods (arda.iam.*).
// These answer "who am I" and "what can I do" from the gateway-injected
// identity context — no additional service call is made.
func RegisterIAMCatalog(reg *DispatcherRegistry) {
	reg.Register(
		CatalogEntry{
			MethodName: "iam.me",
			SDKPath:    "arda.iam.me",
			Domain:     "iam",
			Signature:  "arda.iam.me(): Promise<Me>;",
			JSDoc: `/**
 * Return the current actor's identity: user, tenant, organizations, roles,
 * permissions, and global admin flag. Reads from the gateway-injected
 * identity context; no IAM service call is made.
 * @returns Me { user, tenant, organizations, roles, permissions, globalRoles, isGlobalAdmin }
 * @domain iam
 */`,
			Keywords:            []string{"iam", "me", "whoami", "identity", "profile", "user", "roles", "permissions", "tenant", "account"},
			Kind:                "read",
			RequiredPermissions: []string{"ai.assistant.use"},
			Risk:                "low",
			Timeout:             500 * time.Millisecond,
		},
		func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
			return map[string]any{
				"user": map[string]any{
					"id":       scope.ActorUserID,
					"username": scope.Username,
					"email":    scope.Email,
				},
				"tenant":        map[string]any{"id": scope.TenantID},
				"organizations": scope.OrgIDs,
				"roles":         scope.Roles,
				"permissions":   sortedKeys(scope.Permissions),
				"globalRoles":   scope.GlobalRoles,
				"isGlobalAdmin": scope.GlobalAdmin,
			}, nil
		},
	)

	reg.Register(
		CatalogEntry{
			MethodName: "iam.listCapabilities",
			SDKPath:    "arda.iam.listCapabilities",
			Domain:     "iam",
			Signature:  "arda.iam.listCapabilities(args: { domain?: string; search?: string; kind?: 'read'|'confirm'; limit?: number; cursor?: number }): Promise<CapabilityPage>;",
			JSDoc: `/**
 * List capabilities (SDK methods) the current actor is allowed to use,
 * with optional domain/kind filter, keyword search, and pagination.
 * Use this to discover what the assistant can do in this tenant.
 * @param args.domain Filter by domain: crm, finance, hrm, knowledge, iam, workflow, all (default all)
 * @param args.search Free-text keyword filter (e.g. "export report")
 * @param args.kind Filter by kind: read or confirm (mutations)
 * @param args.limit Page size, 1-50 (default 20)
 * @param args.cursor Zero-based offset for the next page (default 0)
 * @returns CapabilityPage { items: [{ sdkPath, domain, kind, summary, requiredPermissions, risk }], total, hasMore, nextCursor }
 * @domain iam
 */`,
			Keywords:            []string{"iam", "capabilities", "tools", "list", "permissions", "discover", "what can i do", "available"},
			Kind:                "read",
			RequiredPermissions: []string{"ai.assistant.use"},
			Risk:                "low",
			Timeout:             1 * time.Second,
		},
		func(ctx context.Context, scope tools.Context, args map[string]any) (any, error) {
			return listCapabilities(reg, scope, args)
		},
	)
}

func listCapabilities(reg *DispatcherRegistry, scope tools.Context, args map[string]any) (any, error) {
	domain, _ := args["domain"].(string)
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" || domain == "all" {
		domain = ""
	}

	search, _ := args["search"].(string)
	search = strings.TrimSpace(strings.ToLower(search))

	kind, _ := args["kind"].(string)
	kind = strings.TrimSpace(strings.ToLower(kind))

	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
		if limit > 50 {
			limit = 50
		}
	}

	cursor := 0
	if c, ok := args["cursor"].(float64); ok && c > 0 {
		cursor = int(c)
	}

	// Collect entries the actor is allowed to call.
	entries := reg.AllEntries()
	filtered := make([]CatalogEntry, 0, len(entries))
	for _, entry := range entries {
		if domain != "" && !strings.EqualFold(entry.Domain, domain) {
			continue
		}
		if kind != "" && !strings.EqualFold(entry.Kind, kind) {
			continue
		}
		if search != "" && !entryMatches(entry, search) {
			continue
		}
		if err := entry.CheckPermissions(scope); err != nil {
			continue
		}
		filtered = append(filtered, entry)
	}

	// Deterministic ordering for stable pagination.
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].SDKPath < filtered[j].SDKPath
	})

	total := len(filtered)
	start := min(cursor, total)
	end := min(start+limit, total)
	items := make([]map[string]any, 0, end-start)
	for _, entry := range filtered[start:end] {
		items = append(items, map[string]any{
			"sdkPath":             entry.SDKPath,
			"domain":              entry.Domain,
			"kind":                entry.Kind,
			"summary":             firstJSDocLine(entry.JSDoc),
			"requiredPermissions": entry.RequiredPermissions,
			"risk":                entry.Risk,
		})
	}

	return map[string]any{
		"items":      items,
		"total":      total,
		"hasMore":    end < total,
		"nextCursor": end,
	}, nil
}

func entryMatches(entry CatalogEntry, search string) bool {
	haystack := strings.ToLower(strings.Join(entry.Keywords, " ") + " " + entry.SDKPath + " " + entry.JSDoc)
	for _, token := range strings.Fields(search) {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func firstJSDocLine(jsdoc string) string {
	line := strings.TrimSpace(jsdoc)
	if idx := strings.IndexByte(line, '\n'); idx != -1 {
		line = line[:idx]
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, "/**"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
	return line
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
