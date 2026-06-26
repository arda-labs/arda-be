package system

const (
	SuperAdminUserID       = "00000000-0000-0000-0000-000000000002"
	SuperAdminRoleID       = "00000000-0000-0000-0000-000000000002"
	SuperAdminPermissionID = "00000000-0000-0000-0000-00000000000d"

	SuperAdminUsername        = "superadmin"
	SuperAdminEmail           = "superadmin@arda.local"
	SuperAdminDisplayName     = "Super Admin"
	SuperAdminExternalSubject = "super-admin"
	SuperAdminTenantID        = "default"

	SuperAdminRoleCode       = "SUPER_ADMIN"
	SuperAdminPermissionCode = "superadmin"
	SuperAdminLockedStatus   = "LOCKED"
)

// LegacyDefaultAdmin123Hash is the old development hash that must never remain
// active for the system superadmin account without an explicit bootstrap secret.
const LegacyDefaultAdmin123Hash = "$2a$12$LJ3m4ys3Lk0TSwHlvS.JJOvc5sx5GQJfKPdKR0MJfN.ZcJKW5K7iW"
