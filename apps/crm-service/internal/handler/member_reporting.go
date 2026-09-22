package handler

import (
	"net/http"
	"strings"

	"github.com/arda-labs/arda/apps/crm-service/internal/domain"
)

// InternalReportingMembers serves GET /internal/reporting/members for
// statistical-service's reporting ETL (signed caller; tenant re-checked here).
// It carries the capital columns the QCMS membership indicators aggregate.
func (h *MemberHandler) InternalReportingMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeError(w, r, http.StatusBadRequest, "verified tenant scope is required")
		return
	}
	orgCode := strings.TrimSpace(r.URL.Query().Get("org_code"))
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	items, err := h.svc.ListMembersForReporting(r.Context(), tenantID, orgCode)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"as_of": asOf, "items": toReportingMembers(items)})
}

// InternalReportingMemberRequests serves GET /internal/reporting/member-requests
// for statistical-service: the capital-request pipeline (pending + approved) so
// the pipeline indicators (10023/10030-10034) can be computed.
func (h *MemberHandler) InternalReportingMemberRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if tenantID == "" {
		writeError(w, r, http.StatusBadRequest, "verified tenant scope is required")
		return
	}
	items, err := h.svc.ListMemberRequestsForReporting(r.Context(), tenantID)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"as_of": strings.TrimSpace(r.URL.Query().Get("as_of")),
		"items": items,
	})
}

// reportingMember is the member row exposed to the ETL: capital columns plus
// the org/type dimensions; actor ids and audit columns stay behind.
type reportingMember struct {
	MemberCode        string `json:"member_code"`
	CustomerCode      string `json:"customer_code"`
	OrgCode           string `json:"org_code"`
	MemberTypeCode    string `json:"member_type_code"`
	MemberStatus      string `json:"member_status"`
	OpenDate          string `json:"open_date"`
	LeaveDate         string `json:"leave_date,omitempty"`
	EstbCapitalMinor  int64  `json:"estb_capital_minor"`
	AddCapitalMinor   int64  `json:"add_capital_minor"`
	TotalCapitalMinor int64  `json:"total_capital_minor"`
}

func toReportingMembers(items []domain.Member) []reportingMember {
	out := make([]reportingMember, 0, len(items))
	for _, m := range items {
		out = append(out, reportingMember{
			MemberCode:        m.MemberCode,
			CustomerCode:      m.CustomerCode,
			OrgCode:           m.OrgCode,
			MemberTypeCode:    m.MemberTypeCode,
			MemberStatus:      m.MemberStatus,
			OpenDate:          m.OpenDate,
			LeaveDate:         m.LeaveDate,
			EstbCapitalMinor:  m.EstbCapitalMinor,
			AddCapitalMinor:   m.AddCapitalMinor,
			TotalCapitalMinor: m.TotalCapitalMinor,
		})
	}
	return out
}
