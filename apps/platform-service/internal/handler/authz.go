package handler

import (
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
)

// requireGlobalAdmin guards platform-wide control-plane actions (EOD trigger,
// shared geo-admin reference data).
//
// Decision: platform-service performs no authn/authz of its own and must not
// add a parallel policy store. The auth-gateway strips browser-supplied
// identity headers and rewrites the verified ones, so X-Auth-Checked,
// X-Global-Admin, X-Global-Roles and X-Global-Permissions can only originate
// from a verified gateway session. Tenant-scoped X-Roles / X-Permissions
// deliberately do not count: every tenant admin holds platform.manage (the
// gateway route permission), while plt_system_dates and geo_admin_units are
// system-wide state, so a tenant-scoped operator must not mutate them.
// Mirrors iam-service hasGlobalAdminCapability.
func requireGlobalAdmin(w http.ResponseWriter, r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Auth-Checked")), "true") && hasGlobalAdminCapability(r) {
		return true
	}
	writeErrorCode(w, http.StatusForbidden, ardaerrors.CodeForbidden, "global administrator capability is required")
	return false
}

func hasGlobalAdminCapability(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Global-Admin")), "true") {
		return true
	}
	for _, value := range []string{
		r.Header.Get("X-Global-Roles"),
		r.Header.Get("X-Global-Permissions"),
	} {
		for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
			if strings.EqualFold(strings.TrimSpace(item), "SUPER_ADMIN") ||
				strings.EqualFold(strings.TrimSpace(item), "superadmin") {
				return true
			}
		}
	}
	return false
}
