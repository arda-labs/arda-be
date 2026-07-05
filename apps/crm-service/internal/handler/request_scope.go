package handler

import (
	"net/http"
	"strings"
)

type RequestScope struct {
	TenantID    string
	OrgIDs      []string
	ActiveOrgID string
	UserID      string
}

func ScopeFromRequest(r *http.Request) RequestScope {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		tenantID = "default"
	}
	return RequestScope{
		TenantID:    tenantID,
		OrgIDs:      splitCSVHeader(r.Header.Get("X-User-Org-Ids")),
		ActiveOrgID: strings.TrimSpace(r.Header.Get("X-Org-Id")),
		UserID:      strings.TrimSpace(r.Header.Get("X-User-Id")),
	}
}

func (s RequestScope) AllowsOrg(orgID string) bool {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" || len(s.OrgIDs) == 0 {
		return true
	}
	for _, allowed := range s.OrgIDs {
		if allowed == orgID {
			return true
		}
	}
	return false
}

func (s RequestScope) ResolveOrgID() string {
	if s.ActiveOrgID != "" && s.AllowsOrg(s.ActiveOrgID) {
		return s.ActiveOrgID
	}
	if len(s.OrgIDs) == 1 {
		return s.OrgIDs[0]
	}
	return s.ActiveOrgID
}

func splitCSVHeader(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
