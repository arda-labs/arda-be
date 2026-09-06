package handler

import (
	"net/http"
	"strings"
)

// OrgScope is the data-scope the BFF resolved for the session (P1c.3,
// service-boundary-design §5): all organizations the user belongs to plus
// the optional active org (validated server-side by the BFF).
type OrgScope struct {
	ActiveOrgID string
	OrgIDs      []string
}

func orgScopeFromRequest(r *http.Request) OrgScope {
	var ids []string
	if raw := strings.TrimSpace(r.Header.Get("X-User-Org-Ids")); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				ids = append(ids, part)
			}
		}
	}
	return OrgScope{ActiveOrgID: strings.TrimSpace(r.Header.Get("X-Org-Id")), OrgIDs: ids}
}

// ActiveOrg returns the org to stamp on new records: the active org when
// allowed, else the single membership, else "" (global rows for admins).
func (s OrgScope) ActiveOrg() string {
	if s.ActiveOrgID != "" && s.allows(s.ActiveOrgID) {
		return s.ActiveOrgID
	}
	if s.ActiveOrgID == "" && len(s.OrgIDs) == 1 {
		return s.OrgIDs[0]
	}
	return s.ActiveOrgID
}

// ListFilter returns the org codes list queries may see; empty slice means
// unrestricted (global admin / no org model yet).
func (s OrgScope) ListFilter() []string {
	if s.ActiveOrgID != "" && s.allows(s.ActiveOrgID) {
		return []string{s.ActiveOrgID}
	}
	return s.OrgIDs
}

func (s OrgScope) allows(orgID string) bool {
	if orgID == "" {
		return false
	}
	for _, allowed := range s.OrgIDs {
		if allowed == orgID {
			return true
		}
	}
	return false
}
