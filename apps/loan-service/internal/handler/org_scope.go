package handler

import (
	"errors"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
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

var (
	errOrgScopeRequired  = errors.New("organization scope is required")
	errActiveOrgNotAllow = errors.New("active organization is not allowed")
)

// ListFilter returns the org codes list queries may see. It fails closed
// (AP-TEN-01, theo mẫu crm-service RequestScope.Validate): thiếu
// X-User-Org-Ids hoặc X-Org-Id nằm ngoài phạm vi là lỗi, không bao giờ trả
// về danh sách rỗng mà repository hiểu là "mọi đơn vị của tenant".
func (s OrgScope) ListFilter() ([]string, error) {
	if len(s.OrgIDs) == 0 {
		return nil, errOrgScopeRequired
	}
	if s.ActiveOrgID != "" {
		if !s.allows(s.ActiveOrgID) {
			return nil, errActiveOrgNotAllow
		}
		return []string{s.ActiveOrgID}, nil
	}
	return s.OrgIDs, nil
}

// requireOrgListFilter writes 403 and returns false when the org scope is
// missing or invalid.
func requireOrgListFilter(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	orgCodes, err := orgScopeFromRequest(r).ListFilter()
	if err != nil {
		writeErrorCode(w, http.StatusForbidden, ardaerrors.CodeTenantScopeRequired, err.Error())
		return nil, false
	}
	return orgCodes, true
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
