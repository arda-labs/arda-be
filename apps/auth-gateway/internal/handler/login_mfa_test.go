package handler

import (
	"testing"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
)

func TestRequiresLoginMFAForAdminRoles(t *testing.T) {
	handler := &BFFHandler{}

	for _, role := range []string{"ADMIN", "SUPER_ADMIN"} {
		if !handler.requiresLoginMFA(&iamclient.UserContext{Roles: []string{role}}) {
			t.Fatalf("expected role %s to require login MFA", role)
		}
	}
}

func TestRequiresLoginMFAForSuperadminPermission(t *testing.T) {
	handler := &BFFHandler{}

	if !handler.requiresLoginMFA(&iamclient.UserContext{Permissions: []string{"superadmin"}}) {
		t.Fatal("expected superadmin permission to require login MFA")
	}
}

func TestRequiresLoginMFASkipsRegularUsers(t *testing.T) {
	handler := &BFFHandler{}

	if handler.requiresLoginMFA(&iamclient.UserContext{Roles: []string{"USER"}}) {
		t.Fatal("expected regular users to skip login MFA")
	}
}
