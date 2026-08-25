package system

const (
	SuperAdminUserID       = "00000000-0000-0000-0000-000000000002"
	SuperAdminRoleID       = "00000000-0000-0000-0000-000000000002"
	SuperAdminPermissionID = "00000000-0000-0000-0000-00000000000d"

	SuperAdminUsername        = "superadmin"
	SuperAdminEmail           = "superadmin@arda.local"
	SuperAdminDisplayName     = "Super Admin"
	SuperAdminExternalSubject = "super-admin"
	// SuperAdminTenantID is a reserved bootstrap/system tenant. It is used only
	// for the explicitly provisioned super-admin record; request middleware and
	// domain repositories must never use it as a missing-scope fallback.
	SuperAdminTenantID = "default"

	SuperAdminRoleCode       = "SUPER_ADMIN"
	SuperAdminPermissionCode = "superadmin"
)
