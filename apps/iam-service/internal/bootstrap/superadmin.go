package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/iam-service/internal/system"
)

type SuperAdminOptions struct{}

func EnsureSuperAdmin(ctx context.Context, db *sql.DB, opts SuperAdminOptions) error {
	if err := ensureSuperAdminRole(ctx, db); err != nil {
		return err
	}
	if err := ensureSuperAdminPermission(ctx, db); err != nil {
		return err
	}
	if err := ensureSuperAdminUser(ctx, db, opts); err != nil {
		return err
	}
	if err := ensureCasbinSuperAdminPolicy(ctx, db); err != nil {
		return err
	}
	return nil
}

func ensureSuperAdminRole(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO iam_roles (id, code, name, status)
		VALUES ($1, $2, 'Super Administrator', 'ACTIVE')
		ON CONFLICT (code) DO UPDATE SET
			name = EXCLUDED.name,
			status = 'ACTIVE',
			updated_at = now()
	`, system.SuperAdminRoleID, system.SuperAdminRoleCode)
	if err != nil {
		return fmt.Errorf("ensure superadmin role: %w", err)
	}
	return nil
}

func ensureSuperAdminPermission(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO iam_permissions (id, code, name, module_code, resource_code, operation_code)
		VALUES ($1, $2, 'Super Admin Access (wildcard)', 'system', '*', '*')
		ON CONFLICT (code) DO UPDATE SET
			name = EXCLUDED.name,
			module_code = EXCLUDED.module_code,
			resource_code = EXCLUDED.resource_code,
			operation_code = EXCLUDED.operation_code,
			updated_at = now()
	`, system.SuperAdminPermissionID, system.SuperAdminPermissionCode)
	if err != nil {
		return fmt.Errorf("ensure superadmin permission: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO iam_role_permissions (role_id, permission_id)
		VALUES (
			(SELECT id FROM iam_roles WHERE code = $1),
			(SELECT id FROM iam_permissions WHERE code = $2)
		)
		ON CONFLICT DO NOTHING
	`, system.SuperAdminRoleCode, system.SuperAdminPermissionCode)
	if err != nil {
		return fmt.Errorf("assign superadmin permission: %w", err)
	}
	return nil
}

func ensureSuperAdminUser(ctx context.Context, db *sql.DB, opts SuperAdminOptions) error {
	_ = opts

	_, err := db.ExecContext(ctx, `
		INSERT INTO iam_users (
			id, external_subject, username, email, display_name,
			source, status, tenant_id
		)
		VALUES ($1, $2, $3, $4, $5, 'internal', $6, $7)
		ON CONFLICT (username) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			source = EXCLUDED.source,
			status = EXCLUDED.status,
			tenant_id = EXCLUDED.tenant_id,
			updated_at = now()
	`, system.SuperAdminUserID, system.SuperAdminExternalSubject, system.SuperAdminUsername,
		system.SuperAdminEmail, system.SuperAdminDisplayName, "ACTIVE",
		system.SuperAdminTenantID)
	if err != nil {
		return fmt.Errorf("ensure superadmin user: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO iam_user_roles (user_id, role_id)
		VALUES (
			(SELECT id FROM iam_users WHERE username = $1),
			(SELECT id FROM iam_roles WHERE code = $2)
		)
		ON CONFLICT DO NOTHING
	`, system.SuperAdminUsername, system.SuperAdminRoleCode)
	if err != nil {
		return fmt.Errorf("assign superadmin role: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO iam_user_organizations (user_id, organization_id)
		SELECT u.id, o.id
		FROM iam_users u
		JOIN iam_organizations o ON o.code = 'root'
		WHERE u.username = $1
		ON CONFLICT DO NOTHING
	`, system.SuperAdminUsername)
	if err != nil {
		slog.Warn("assign superadmin root org skipped", "err", err)
	}

	return nil
}

func ensureCasbinSuperAdminPolicy(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3)
		SELECT 'p', $1, '*', '*', 'allow'
		WHERE NOT EXISTS (
			SELECT 1 FROM iam_casbin_rules
			WHERE ptype = 'p' AND v0 = $1 AND v1 = '*' AND v2 = '*' AND v3 = 'allow'
		)
	`, system.SuperAdminRoleCode)
	if err != nil {
		return fmt.Errorf("ensure superadmin casbin wildcard policy: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO iam_casbin_rules (ptype, v0, v1, v2, v3)
		SELECT 'p', $1, '*:*', '*', 'allow'
		WHERE NOT EXISTS (
			SELECT 1 FROM iam_casbin_rules
			WHERE ptype = 'p' AND v0 = $1 AND v1 = '*:*' AND v2 = '*' AND v3 = 'allow'
		)
	`, system.SuperAdminRoleCode)
	if err != nil {
		return fmt.Errorf("ensure superadmin casbin resource policy: %w", err)
	}
	return nil
}
