package http

import (
	"net/http"

	"github.com/arda-labs/arda/apps/iam-service/internal/handler"
)

// NewRouter wires HTTP routes for the IAM service.
func NewRouter(userHandler *handler.UserHandler, authHandler *handler.AuthHandler, policyHandler *handler.PolicyHandler, adminHandler *handler.AdminHandler, sessionHandler *handler.SessionHandler, mfaHandler *handler.MFAHandler, auditHandler *handler.AuditHandler) http.Handler {
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

	// ── Auth API (public) ──
	mux.HandleFunc("/api/auth/login-page", method("GET", authHandler.LoginPage))
	mux.HandleFunc("/api/auth/login/password", method("POST", authHandler.LoginPassword))
	mux.HandleFunc("/api/auth/login/external", method("POST", authHandler.LoginExternal))
	mux.HandleFunc("/api/auth/callback/{provider_id}", method("GET", authHandler.CallbackProvider))
	mux.HandleFunc("/api/auth/callback", method("POST", authHandler.CallbackToken))
	mux.HandleFunc("/api/auth/refresh", method("POST", authHandler.Refresh))
	mux.HandleFunc("/api/auth/providers", method("GET", authHandler.ListProviders))
	mux.HandleFunc("/api/auth/consent", method("POST", authHandler.Consent))
	mux.HandleFunc("/api/auth/register", method("POST", authHandler.RegisterUser))

	// ── Admin API — User management ──
	mux.HandleFunc("/api/admin/users/create", method("POST", adminHandler.CreateUser))
	mux.HandleFunc("/api/admin/users", method("GET", adminHandler.ListUsers))
	mux.HandleFunc("/api/admin/users/{id}/sessions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sessionHandler.AdminListUserSessions(w, r)
		case http.MethodDelete:
			sessionHandler.AdminRevokeUserSessions(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/admin/users/{id}/delete", method("DELETE", adminHandler.DeleteUser))
	mux.HandleFunc("/api/admin/users/{id}/disable", method("POST", adminHandler.DisableUser))
	mux.HandleFunc("/api/admin/users/{id}/enable", method("POST", adminHandler.EnableUser))
	mux.HandleFunc("/api/admin/users/{id}", method("GET", adminHandler.GetUser))
	mux.HandleFunc("/api/admin/users/{userId}/roles/{roleId}/remove", method("DELETE", adminHandler.UnassignUserRole))
	mux.HandleFunc("/api/admin/users/roles/assign", method("POST", adminHandler.AssignUserRole))

	// ── Admin API — Role management ──
	mux.HandleFunc("/api/admin/roles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListRoles(w, r)
		case http.MethodPost:
			adminHandler.CreateRole(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
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
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/admin/roles/{id}/permissions", method("GET", adminHandler.ListRolePermissions))
	mux.HandleFunc("/api/admin/roles/{id}/permissions/assign", method("POST", adminHandler.AssignRolePermission))
	mux.HandleFunc("/api/admin/roles/{id}/permissions/{permId}", method("DELETE", adminHandler.UnassignRolePermission))

	// ── Admin API — Permission management ──
	mux.HandleFunc("/api/admin/permissions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminHandler.ListPermissions(w, r)
		case http.MethodPost:
			adminHandler.CreatePermission(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/admin/permissions/{id}", method("DELETE", adminHandler.DeletePermission))

	// ── User API ──
	mux.HandleFunc("/api/iam/me", method("GET", userHandler.Me))
	mux.HandleFunc("/api/iam/me/profile/avatar", method("POST", userHandler.UpdateMyAvatar))

	// ── Policy API ──
	if policyHandler != nil {
		mux.HandleFunc("/api/policy/enforce", method("POST", policyHandler.Enforce))
		mux.HandleFunc("/api/admin/policies", method("GET", policyHandler.ListPolicies))
		mux.HandleFunc("/api/admin/policies/add", method("POST", policyHandler.AddPolicy))
		mux.HandleFunc("/api/admin/policies/remove", method("POST", policyHandler.RemovePolicy))
	}

	// ── Audit API ──
	mux.HandleFunc("/api/admin/audit", method("GET", auditHandler.Query))
	mux.HandleFunc("/api/admin/audit/stats", method("GET", auditHandler.Stats))
	mux.HandleFunc("/api/admin/audit/verify", method("GET", auditHandler.Verify))

	// ── Internal API (service-to-service) ──
	mux.HandleFunc("/internal/iam/users/by-subject/{subject}", method("GET", userHandler.GetBySubject))
	mux.HandleFunc("/internal/iam/users/by-id/{id}/context", method("GET", userHandler.GetContextByID))
	mux.HandleFunc("/internal/iam/sessions", method("POST", sessionHandler.InternalCreateSession))
	mux.HandleFunc("/internal/iam/sessions/{id}", method("DELETE", sessionHandler.InternalRevokeSession))

	// ── Session API ──
	mux.HandleFunc("/api/iam/me/sessions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sessionHandler.ListMySessions(w, r)
		case http.MethodDelete:
			sessionHandler.RevokeMyOtherSessions(w, r)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
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
	mux.HandleFunc("/api/iam/me/mfa/verify", method("POST", mfaHandler.VerifyCode))
	mux.HandleFunc("/api/iam/me/mfa/backup", method("POST", mfaHandler.VerifyBackupCode))
	mux.HandleFunc("/api/admin/users/{id}/mfa/reset", method("POST", mfaHandler.AdminResetMFA))

	return mux
}

func method(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}
