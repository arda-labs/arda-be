package usercontext

import (
	"net/http"
	"testing"
)

func TestFromHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("X-User-Id", " user_1 ")
	h.Set("X-User-Subject", " subject_1 ")
	h.Set("X-Username", " admin ")
	h.Set("X-Tenant-Id", " tenant_1 ")
	h.Set("X-Roles", "admin, approver, ")
	h.Set("X-Permissions", "iam.users.read, platform.lookups.write")

	uc := FromHeaders(h)
	if uc.UserID != "user_1" || uc.Subject != "subject_1" || uc.Username != "admin" || uc.TenantID != "tenant_1" {
		t.Fatalf("unexpected context: %#v", uc)
	}
	if len(uc.Roles) != 2 || uc.Roles[0] != "admin" || uc.Roles[1] != "approver" {
		t.Fatalf("unexpected roles: %#v", uc.Roles)
	}
	if len(uc.Permissions) != 2 || uc.Permissions[0] != "iam.users.read" || uc.Permissions[1] != "platform.lookups.write" {
		t.Fatalf("unexpected permissions: %#v", uc.Permissions)
	}
}
