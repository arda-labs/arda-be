package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/handler"
	ardaerrors "github.com/arda-labs/arda/libs/go/arda-errors"
	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	ardahttp "github.com/arda-labs/arda/libs/go/arda-http"
)

// NewRouter wires HTTP routes for the IAM service.
func NewRouter(userHandler *handler.UserHandler, policyHandler *handler.PolicyHandler, adminHandler *handler.AdminHandler, sessionHandler *handler.SessionHandler, mfaHandler *handler.MFAHandler, auditHandler *handler.AuditHandler, tenantHandlers ...*handler.TenantHandler) http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	// ── Admin API — User management ──
	mux.HandleFunc("/api/admin/users/export", method("GET", adminHandler.ExportUsers))
	mux.HandleFunc("/api/admin/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListUsers(w, r)
		case http.MethodPost:
			adminHandler.CreateUser(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/users/{id}/sessions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sessionHandler.AdminListUserSessions(w, r)
		case http.MethodDelete:
			sessionHandler.AdminRevokeUserSessions(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/users/{id}/status", method("PUT", adminHandler.SetUserStatus))
	mux.HandleFunc("/api/admin/users/{id}/identity/provision", method("POST", adminHandler.ProvisionUserIdentity))
	mux.HandleFunc("/api/admin/users/{id}/identity/password/reset", method("POST", adminHandler.ResetUserPassword))
	mux.HandleFunc("/api/admin/identity/consistency", method("GET", adminHandler.AuditIdentityConsistency))
	mux.HandleFunc("/api/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.GetUser(w, r)
		case http.MethodPut:
			adminHandler.UpdateUser(w, r)
		case http.MethodDelete:
			adminHandler.DeleteUser(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/users/{userId}/roles", method("POST", adminHandler.AssignUserRole))
	mux.HandleFunc("/api/admin/users/{userId}/roles/{roleId}", method("DELETE", adminHandler.UnassignUserRole))
	mux.HandleFunc("/api/admin/users/{id}/groups", method("GET", adminHandler.ListUserGroups))

	// Admin API - Group management
	mux.HandleFunc("/api/admin/groups/export", method("GET", adminHandler.ExportGroups))
	mux.HandleFunc("/api/admin/groups", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListGroups(w, r)
		case http.MethodPost:
			adminHandler.CreateGroup(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/groups/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.GetGroup(w, r)
		case http.MethodPut:
			adminHandler.UpdateGroup(w, r)
		case http.MethodDelete:
			adminHandler.DeleteGroup(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/groups/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListGroupMembers(w, r)
		case http.MethodPost:
			adminHandler.AddGroupMember(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/groups/{id}/members/{userId}", method("DELETE", adminHandler.RemoveGroupMember))
	mux.HandleFunc("/api/admin/groups/{id}/roles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListGroupRoles(w, r)
		case http.MethodPost:
			adminHandler.AssignGroupRole(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/groups/{id}/roles/{roleId}", method("DELETE", adminHandler.UnassignGroupRole))

	// ── Admin API — Role management ──
	mux.HandleFunc("/api/admin/roles/export", method("GET", adminHandler.ExportRoles))
	mux.HandleFunc("/api/admin/roles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListRoles(w, r)
		case http.MethodPost:
			adminHandler.CreateRole(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/roles/{id}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.GetRole(w, r)
		case http.MethodPut:
			adminHandler.UpdateRole(w, r)
		case http.MethodDelete:
			adminHandler.DeleteRole(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/roles/{id}/permissions", method("GET", adminHandler.ListRolePermissions))
	mux.HandleFunc("/api/admin/roles/{id}/permissions/assign", method("POST", adminHandler.AssignRolePermission))
	mux.HandleFunc("/api/admin/roles/{id}/permissions/{permId}", method("DELETE", adminHandler.UnassignRolePermission))

	// ── Admin API — Permission management ──
	mux.HandleFunc("/api/admin/permissions/export", method("GET", adminHandler.ExportPermissions))
	mux.HandleFunc("/api/admin/permissions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListPermissions(w, r)
		case http.MethodPost:
			adminHandler.CreatePermission(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/admin/permissions/{id}", method("DELETE", adminHandler.DeletePermission))

	// ── User API ──
	mux.HandleFunc("/api/iam/me", method("GET", userHandler.Me))
	mux.HandleFunc("/api/iam/me/profile/avatar", method("POST", userHandler.UpdateMyAvatar))
	mux.HandleFunc("/api/iam/me/profile/cover", method("POST", userHandler.UpdateMyCover))
	mux.HandleFunc("/api/iam/me/profile", method("PUT", userHandler.UpdateMyProfile))
	mux.HandleFunc("/api/identity/me/email", method("PUT", userHandler.UpdateMyEmail))
	mux.HandleFunc("/api/identity/me/password", method("PUT", userHandler.UpdateMyPassword))

	// ── Policy API ──
	if policyHandler != nil {
		mux.HandleFunc("/api/policy/enforce", method("POST", policyHandler.Enforce))
		mux.HandleFunc("/api/admin/policies", method("GET", policyHandler.ListPolicies))
		mux.HandleFunc("/api/admin/policies/add", method("POST", policyHandler.AddPolicy))
		mux.HandleFunc("/api/admin/policies/remove", method("POST", policyHandler.RemovePolicy))
	}

	// ── Audit API ──
	mux.HandleFunc("/api/admin/audit/export", method("GET", auditHandler.ExportAudit))
	mux.HandleFunc("/api/admin/audit", method("GET", auditHandler.Query))
	mux.HandleFunc("/api/admin/audit/stats", method("GET", auditHandler.Stats))
	mux.HandleFunc("/api/admin/audit/verify", method("GET", auditHandler.Verify))

	// ── Internal API (service-to-service) ──
	mux.Handle("/internal/iam/users/{id}/mfa/check", internalService(method("POST", mfaHandler.CheckMFA)))
	mux.Handle("/internal/iam/users/by-subject/{subject}", internalService(method("GET", userHandler.GetBySubject)))
	mux.Handle("/internal/iam/users/by-id/{id}/context", internalService(method("GET", userHandler.GetContextByID)))
	mux.Handle("/internal/iam/users/by-kratos-identity/{identityId}/context", internalService(method("GET", userHandler.GetContextByKratosIdentityID)))
	mux.Handle("/internal/iam/users/resolve-kratos-identity", internalService(method("POST", userHandler.ResolveOrLinkKratosIdentity)))
	mux.Handle("/internal/iam/users/resolve-identity", internalService(method("POST", userHandler.ResolveOrLinkIdentity)))
	mux.Handle("/internal/iam/sessions", internalService(method("POST", sessionHandler.InternalCreateSession)))
	mux.Handle("/internal/iam/sessions/{id}", internalService(method("DELETE", sessionHandler.InternalRevokeSession)))

	// Internal AI surface: ai-service calls here with a signed caller
	// assertion and the delegated subject as headers. ListUsers re-validates
	// the delegated actor/tenant scope before serving (requiredAdminTargetTenant).
	mux.Handle("/internal/ai/users", internalAIService(method("GET", adminHandler.ListUsers)))

	// ── Session API ──
	mux.HandleFunc("/api/iam/me/sessions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sessionHandler.ListMySessions(w, r)
		case http.MethodDelete:
			sessionHandler.RevokeMyOtherSessions(w, r)
		default:
			writeMethodNotAllowed(w, r)
		}
	})
	mux.HandleFunc("/api/iam/me/sessions/{id}", method("DELETE", sessionHandler.RevokeMySession))

	// Device API
	mux.HandleFunc("/api/iam/me/devices", method("GET", sessionHandler.ListMyDevices))
	mux.HandleFunc("/api/iam/me/devices/{id}", method("DELETE", sessionHandler.DeleteMyDevice))
	mux.HandleFunc("/api/iam/me/devices/{id}/trust", method("POST", sessionHandler.TrustMyDevice))

	// Session config
	mux.HandleFunc("/api/iam/session/config", method("GET", sessionHandler.SessionConfig))

	// ── MFA API ──
	mux.HandleFunc("/api/iam/me/mfa/enroll", method("POST", mfaHandler.GenerateSecret))
	mux.HandleFunc("/api/iam/me/mfa/verify-enroll", method("POST", mfaHandler.VerifyEnroll))
	mux.HandleFunc("/api/iam/me/mfa/status", method("GET", mfaHandler.MFAStatus))
	mux.HandleFunc("/api/iam/me/mfa/reset", method("POST", mfaHandler.ResetMyMFA))
	mux.HandleFunc("/api/iam/me/mfa/verify", method("POST", mfaHandler.VerifyCode))
	mux.HandleFunc("/api/iam/me/mfa/backup", method("POST", mfaHandler.VerifyBackupCode))
	mux.HandleFunc("/api/admin/users/{id}/mfa/reset", method("POST", mfaHandler.AdminResetMFA))

	// Tenant registry and membership context.
	if len(tenantHandlers) > 0 && tenantHandlers[0] != nil {
		tenantHandler := tenantHandlers[0]
		mux.HandleFunc("/api/admin/tenants", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				tenantHandler.ListAdmin(w, r)
			case http.MethodPost:
				tenantHandler.Create(w, r)
			default:
				writeMethodNotAllowed(w, r)
			}
		})
		mux.HandleFunc("/api/admin/tenants/{tenant_id}/members", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				tenantHandler.ListMembers(w, r)
			case http.MethodPost:
				tenantHandler.AddMember(w, r)
			default:
				writeMethodNotAllowed(w, r)
			}
		})
		mux.HandleFunc("/api/admin/tenants/{tenant_id}/members/{user_id}", method("DELETE", tenantHandler.RemoveMember))
		mux.HandleFunc("/api/iam/me/tenants", method("GET", tenantHandler.ListMine))
	}

	return mux
}

// internalService authenticates the auth-gateway -> IAM HTTP adapter with the
// same short-lived workload assertion used by internal gRPC. Browser/BFF
// routes intentionally do not pass through this middleware.
func internalService(next http.Handler) http.Handler {
	secret, secretErr := identity.SecretFromEnv()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if secretErr != nil {
			ardahttp.WriteProblem(w, r, http.StatusServiceUnavailable, ardaerrors.New(ardaerrors.CodeInternal, "internal service identity is not configured"))
			return
		}
		token := strings.TrimSpace(r.Header.Get("X-Service-Auth"))
		claims, err := identity.Verify(token, secret, "iam-service", time.Now())
		if err != nil || claims.Source != "auth-gateway" {
			ardahttp.WriteProblem(w, r, http.StatusUnauthorized, ardaerrors.New(ardaerrors.CodeUnauthorized, "valid auth-gateway service identity is required"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// internalAIService authenticates the ai-service caller on the internal AI
// surface. Missing/invalid tokens are hard-rejected; the delegated subject
// (X-Tenant-Id, X-User-Id, ...) is forwarded by the caller, not trusted from
// browsers — this route is never exposed to them.
func internalAIService(next http.Handler) http.Handler {
	secret, err := identity.SecretFromEnv()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ardahttp.WriteProblem(w, r, http.StatusServiceUnavailable, ardaerrors.New(ardaerrors.CodeInternal, "internal service identity is not configured"))
		})
	}
	return identity.RequireServiceAuth(secret, "iam-service", identity.AllowedSources("ai-service"))(next)
}

func method(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeMethodNotAllowed(w, r)
			return
		}
		next(w, r)
	}
}

func writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	ardahttp.WriteProblem(w, r, http.StatusMethodNotAllowed, ardaerrors.New(ardaerrors.CodeMethodNotAllowed, "method not allowed"))
}
