package events

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// RecipientDirectory is implemented by the IAM and HRM gRPC clients.
type RecipientDirectory interface {
	ResolveNotificationRecipients(context.Context, string, []string, []string, []string) ([]string, error)
	ListIAMUsersByOrgUnit(context.Context, string, string, bool) ([]string, error)
}

type RecipientSelectors struct {
	UserIDs            []string `json:"user_ids"`
	GroupIDs           []string `json:"group_ids"`
	RoleCodes          []string `json:"role_codes"`
	OrgUnitIDs         []string `json:"org_unit_ids"`
	IncludeDescendants bool     `json:"include_descendants"`
}

// ResolveRecipients unions explicit users with IAM and HRM selectors. When
// both role and org-unit selectors exist, those two sets intersect so the
// notification reaches users who satisfy both business constraints.
func ResolveRecipients(ctx context.Context, directory RecipientDirectory, tenantID string, selectors RecipientSelectors, managementGroup string) ([]string, error) {
	if directory == nil {
		return nil, fmt.Errorf("recipient directory is not configured")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required for recipient resolution")
	}
	baseUsers, err := resolveIAMBatches(ctx, directory, tenantID, selectors.UserIDs, selectors.GroupIDs, nil)
	if err != nil {
		return nil, fmt.Errorf("resolve IAM recipients: %w", err)
	}
	roleUsers, err := resolveIAMBatches(ctx, directory, tenantID, nil, nil, selectors.RoleCodes)
	if err != nil {
		return nil, fmt.Errorf("resolve IAM role recipients: %w", err)
	}
	orgUsers := []string{}
	for _, unit := range cleanUserIDs(selectors.OrgUnitIDs) {
		resolved, resolveErr := directory.ListIAMUsersByOrgUnit(ctx, tenantID, unit, selectors.IncludeDescendants)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve HRM org unit %s: %w", unit, resolveErr)
		}
		orgUsers = append(orgUsers, resolved...)
	}
	orgUsers = cleanUserIDs(orgUsers)
	if len(baseUsers) > 1000 || len(roleUsers) > 1000 || len(orgUsers) > 1000 {
		return resolveManagementGroup(ctx, directory, tenantID, managementGroup)
	}
	selected := append([]string(nil), baseUsers...)
	if len(selectors.RoleCodes) > 0 && len(orgUsers) > 0 {
		selected = append(selected, intersectUsers(roleUsers, orgUsers)...)
	} else {
		selected = append(selected, roleUsers...)
		selected = append(selected, orgUsers...)
	}
	selected = cleanUserIDs(selected)
	if len(selected) > 1000 {
		return resolveManagementGroup(ctx, directory, tenantID, managementGroup)
	}
	return selected, nil
}

func resolveManagementGroup(ctx context.Context, directory RecipientDirectory, tenantID, managementGroup string) ([]string, error) {
	group := strings.TrimSpace(managementGroup)
	if group == "" {
		return nil, fmt.Errorf("notification.recipient_overflow: recipient set exceeds 1000 and no management group is configured")
	}
	selected, err := resolveIAMBatches(ctx, directory, tenantID, nil, []string{group}, nil)
	if err != nil {
		return nil, fmt.Errorf("notification.recipient_overflow: resolve management group: %w", err)
	}
	selected = cleanUserIDs(selected)
	if len(selected) > 1000 {
		return nil, fmt.Errorf("notification.recipient_overflow: configured management group exceeds 1000 recipients")
	}
	return selected, nil
}

func resolveIAMBatches(ctx context.Context, directory RecipientDirectory, tenantID string, userIDs, groupIDs, roleCodes []string) ([]string, error) {
	users, groups, roles := cleanUserIDs(userIDs), cleanUserIDs(groupIDs), cleanUserIDs(roleCodes)
	resolved := []string{}
	for ui, gi, ri := 0, 0, 0; ui < len(users) || gi < len(groups) || ri < len(roles); {
		uEnd, gEnd, rEnd := ui+200, gi+200, ri+200
		if uEnd > len(users) {
			uEnd = len(users)
		}
		if gEnd > len(groups) {
			gEnd = len(groups)
		}
		if rEnd > len(roles) {
			rEnd = len(roles)
		}
		u, err := directory.ResolveNotificationRecipients(ctx, tenantID, users[ui:uEnd], groups[gi:gEnd], roles[ri:rEnd])
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, u...)
		ui, gi, ri = uEnd, gEnd, rEnd
	}
	return cleanUserIDs(resolved), nil
}

func cleanUserIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func intersectUsers(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, id := range b {
		set[id] = struct{}{}
	}
	out := make([]string, 0)
	for _, id := range a {
		if _, ok := set[id]; ok {
			out = append(out, id)
		}
	}
	return cleanUserIDs(out)
}
