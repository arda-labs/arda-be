package metadata

import "net/http"

// AppendToHTTP writes the delegated subject context onto an outgoing HTTP
// request. These headers describe who the caller is acting *for* (the user,
// tenant, org), separate from the signed caller identity carried in
// x-service-auth by the caller's transport.
//
// The receiving service must treat these as delegated context, not as an
// authorization decision — it remains responsible for resource-level checks.
func AppendToHTTP(h http.Header, m Context) {
	set := func(key, value string) {
		if value != "" {
			h.Set(key, value)
		}
	}
	set(RequestID, m.RequestID)
	set(TraceID, m.TraceID)
	set(TraceParent, m.TraceParent)
	set(TenantID, m.TenantID)
	set(UserID, m.UserID)
	set(ActorUserID, m.ActorUserID)
	set(TargetUserID, m.TargetUserID)
	set(UserSubject, m.UserSubject)
	set(OrgID, m.OrgID)
	set(OrgIDs, joinCSV(m.OrgIDs))
	set(GroupIDs, joinCSV(m.GroupIDs))
	set(Roles, joinCSV(m.Roles))
	set(Permissions, joinCSV(m.Permissions))
	set(AuthRisk, m.AuthRisk)
	set(SourceService, m.SourceService)
	set(IdempotencyKey, m.IdempotencyKey)
	set(ServiceAccount, m.ServiceAccount)
	// AuthChecked marks that the request has been through a trusted auth
	// boundary (the gateway) before being delegated to this service.
	if m.AuthChecked != "" {
		h.Set(AuthChecked, m.AuthChecked)
	}
}

func joinCSV(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := ""
	for i, v := range values {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
