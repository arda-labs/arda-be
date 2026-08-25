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
	// domain repositories must never use it as a missing-scope fallback. It must
	// not use the legacy "default" placeholder, which is rejected for new IAM
	// writes by the explicit tenant-scope migration.
	SuperAdminTenantID = "system"
	// InitialBusinessTenantID is the stable tenant created by the tenant
	// registry migration for legacy single-tenant data.
	InitialBusinessTenantID = "00000000-0000-0000-0000-000000000010"

	SuperAdminRoleCode       = "SUPER_ADMIN"
	SuperAdminPermissionCode = "superadmin"
)
