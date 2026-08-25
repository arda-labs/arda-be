package handler

import (
	"net"
	"net/http"
	"strings"

	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

func respondJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	ardahttp.WriteJSON(w, r, status, data)
}

func respondError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	respondRequestError(w, r, status, errorCodeFor(status, msg), msg)
}

func respondErrorCode(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	respondRequestError(w, r, status, code, msg)
}

func respondRequestError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	ardahttp.WriteErrorCode(w, r, status, code, msg)
}

func errorCodeFor(status int, msg string) string {
	code := ardaerrors.CodeForStatus(status)
	lower := strings.ToLower(msg)
	switch {
	case status == http.StatusBadRequest && strings.Contains(lower, "json"):
		return ardaerrors.CodeInvalidJSON
	case status == http.StatusBadRequest && strings.Contains(lower, "required"):
		return ardaerrors.CodeRequired
	case status == http.StatusNotFound:
		return ardaerrors.CodeNotFound
	case status == http.StatusConflict:
		return ardaerrors.CodeConflict
	case status == http.StatusUnauthorized:
		return ardaerrors.CodeUnauthorized
	case status == http.StatusForbidden:
		return ardaerrors.CodeForbidden
	default:
		return code
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// requiredAdminTargetTenant makes the managed resource scope explicit for
// admin path operations. Authentication identifies the actor; this value
// identifies the tenant containing the resource being managed.
func requiredAdminTargetTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("tenant_id"), r.URL.Query().Get("tenantId")))
	return validateAdminTargetTenant(w, r, tenantID)
}

// validateAdminTargetTenant makes both sides of a management decision
// explicit: the request identifies an actor through the verified gateway
// context, while the target tenant is supplied by the management operation.
// A normal tenant administrator may manage resources in that same tenant;
// cross-tenant management is reserved for the explicitly named global
// capability. No missing header or browser-supplied identity is accepted as a
// fallback.
func validateAdminTargetTenant(w http.ResponseWriter, r *http.Request, tenantID string) (string, bool) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		respondAdminError(w, r, http.StatusBadRequest, "tenant_id is required for the target resource")
		return "", false
	}

	if r.Header.Get("X-Auth-Checked") != "true" || strings.TrimSpace(r.Header.Get("X-User-Id")) == "" {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeForbidden, "verified actor scope is required")
		return "", false
	}
	actorTenant := strings.TrimSpace(r.Header.Get("X-Tenant-Id"))
	if actorTenant == "" {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeForbidden, "verified actor tenant is required")
		return "", false
	}
	if tenantID != actorTenant && !hasGlobalAdminCapability(r) {
		respondAdminRequestErrorCode(w, r, http.StatusForbidden, ardaerrors.CodeForbidden, "actor cannot manage the target tenant")
		return "", false
	}
	return tenantID, true
}

func hasGlobalAdminCapability(r *http.Request) bool {
	for _, value := range []string{r.Header.Get("X-Roles"), r.Header.Get("X-Permissions")} {
		for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
			if strings.EqualFold(strings.TrimSpace(item), "SUPER_ADMIN") || strings.EqualFold(strings.TrimSpace(item), "superadmin") {
				return true
			}
		}
	}
	return false
}

func parseAdminListQuery(r *http.Request) ardahttp.ListQuery {
	return ardahttp.ParseListQuery(r.URL.Query())
}

func listSortOrder(listQuery ardahttp.ListQuery) string {
	if strings.EqualFold(listQuery.Order, "asc") {
		return "ASC"
	}
	return "DESC"
}

func respondAdminList[T any](w http.ResponseWriter, r *http.Request, items []T, total, page, perPage int) {
	ardahttp.WriteSuccess(w, r, http.StatusOK, ardahttp.NewListResponse(page, perPage, total, items))
}

// Admin and public self-service endpoints use the canonical application
// envelope. Auth-provider and internal IAM protocol handlers keep their
// protocol-specific response shape and use the generic helpers above.
func respondAdminJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	ardahttp.WriteSuccess(w, r, status, data)
}

func respondAdminJSONWithRequest(w http.ResponseWriter, r *http.Request, status int, data any) {
	respondAdminJSON(w, r, status, data)
}

func respondAdminError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	respondAdminRequestError(w, r, status, errorCodeFor(status, msg), msg)
}

func respondAdminRequestError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, msg))
}

func respondAdminRequestErrorCode(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	respondAdminRequestError(w, r, status, code, msg)
}

func respondCanonicalJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	ardahttp.WriteSuccess(w, r, status, data)
}

func respondCanonicalError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	respondCanonicalRequestError(w, r, status, errorCodeFor(status, msg), msg)
}

func respondCanonicalRequestError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	ardahttp.WriteProblem(w, r, status, ardaerrors.New(code, msg))
}

// extractIP extracts the client IP from request headers or remote address.
func extractIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip, _, _ := strings.Cut(fwd, ",")
		return normalizeIP(strings.TrimSpace(ip))
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return normalizeIP(host)
	}
	return normalizeIP(r.RemoteAddr)
}

func normalizeIP(value string) string {
	return strings.Trim(value, "[]")
}
