package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

type TenantRepository struct {
	db *sql.DB
}

func NewTenantRepository(db *sql.DB) *TenantRepository {
	return &TenantRepository{db: db}
}

func (r *TenantRepository) List(ctx context.Context) ([]domain.Tenant, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM iam_tenants
		WHERE status <> 'DELETING'
		ORDER BY code
	`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []domain.Tenant
	for rows.Next() {
		var tenant domain.Tenant
		if err := rows.Scan(&tenant.ID, &tenant.Code, &tenant.Name, &tenant.Status, &tenant.CreatedAt, &tenant.UpdatedAt); err != nil {
			return nil, err
		}
		tenants = append(tenants, tenant)
	}
	return tenants, rows.Err()
}

func (r *TenantRepository) GetForUser(ctx context.Context, userID, tenantID string) (*domain.TenantMembership, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT t.id, t.code, t.name, t.status, tm.status, tm.is_default
		FROM iam_tenant_memberships tm
		JOIN iam_tenants t ON t.id = tm.tenant_id
		WHERE tm.user_id = $1 AND tm.tenant_id = $2
		  AND tm.status = 'ACTIVE' AND t.status = 'ACTIVE'
	`, userID, tenantID)
	var membership domain.TenantMembership
	if err := row.Scan(&membership.TenantID, &membership.TenantCode, &membership.TenantName, &membership.TenantStatus, &membership.Status, &membership.IsDefault); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get tenant membership: %w", err)
	}
	return &membership, nil
}

func (r *TenantRepository) Exists(ctx context.Context, tenantID string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM iam_tenants WHERE id = $1 AND status = 'ACTIVE')
	`, tenantID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check tenant: %w", err)
	}
	return exists, nil
}

func (r *TenantRepository) EnsureMembership(ctx context.Context, userID, tenantID string, isDefault bool) error {
	exists, err := r.Exists(ctx, tenantID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("tenant does not exist or is not active")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin membership transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if isDefault {
		if _, err := tx.ExecContext(ctx, `
			UPDATE iam_tenant_memberships SET is_default = false, updated_at = now()
			WHERE user_id = $1 AND is_default
		`, userID); err != nil {
			return fmt.Errorf("clear default tenant membership: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO iam_tenant_memberships (tenant_id, user_id, status, is_default)
		VALUES ($1, $2, 'ACTIVE', $3)
		ON CONFLICT (tenant_id, user_id) DO UPDATE SET
			status = 'ACTIVE', is_default = EXCLUDED.is_default, updated_at = now()
	`, tenantID, userID, isDefault); err != nil {
		return fmt.Errorf("ensure tenant membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit membership transaction: %w", err)
	}
	return nil
}

func (r *TenantRepository) ListForUser(ctx context.Context, userID string) ([]domain.TenantMembership, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id, t.code, t.name, t.status, tm.status, tm.is_default
		FROM iam_tenant_memberships tm
		JOIN iam_tenants t ON t.id = tm.tenant_id
		WHERE tm.user_id = $1 AND tm.status = 'ACTIVE' AND t.status = 'ACTIVE'
		ORDER BY tm.is_default DESC, t.code
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user tenant memberships: %w", err)
	}
	defer rows.Close()

	var memberships []domain.TenantMembership
	for rows.Next() {
		var membership domain.TenantMembership
		if err := rows.Scan(&membership.TenantID, &membership.TenantCode, &membership.TenantName, &membership.TenantStatus, &membership.Status, &membership.IsDefault); err != nil {
			return nil, err
		}
		memberships = append(memberships, membership)
	}
	return memberships, rows.Err()
}

func (r *TenantRepository) ListMembers(ctx context.Context, tenantID string) ([]domain.TenantMember, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, u.display_name, tm.status, tm.is_default
		FROM iam_tenant_memberships tm
		JOIN iam_users u ON u.id = tm.user_id
		WHERE tm.tenant_id = $1 AND tm.status = 'ACTIVE'
		ORDER BY u.username, u.email
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant members: %w", err)
	}
	defer rows.Close()

	var members []domain.TenantMember
	for rows.Next() {
		var member domain.TenantMember
		if err := rows.Scan(&member.UserID, &member.Username, &member.Email, &member.DisplayName, &member.Status, &member.IsDefault); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (r *TenantRepository) RemoveMembership(ctx context.Context, tenantID, userID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remove tenant membership: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var wasDefault bool
	err = tx.QueryRowContext(ctx, `
		SELECT is_default
		FROM iam_tenant_memberships
		WHERE tenant_id = $1 AND user_id = $2 AND status = 'ACTIVE'
		FOR UPDATE
	`, tenantID, userID).Scan(&wasDefault)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("tenant membership not found")
		}
		return fmt.Errorf("find tenant membership: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE iam_tenant_memberships
		SET status = 'REMOVED', is_default = false, updated_at = now()
		WHERE tenant_id = $1 AND user_id = $2 AND status = 'ACTIVE'
	`, tenantID, userID); err != nil {
		return fmt.Errorf("remove tenant membership: %w", err)
	}
	if wasDefault {
		_, err = tx.ExecContext(ctx, `
			UPDATE iam_tenant_memberships
			SET is_default = true, updated_at = now()
			WHERE user_id = $1 AND tenant_id = (
				SELECT tenant_id FROM iam_tenant_memberships
				WHERE user_id = $1 AND status = 'ACTIVE'
				ORDER BY created_at, user_id
				LIMIT 1
			)
		`, userID)
		if err != nil {
			return fmt.Errorf("promote replacement tenant membership: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remove tenant membership: %w", err)
	}
	return nil
}

func (r *TenantRepository) Create(ctx context.Context, tenant *domain.Tenant, ownerUserID string) error {
	if tenant == nil || strings.TrimSpace(tenant.Code) == "" || strings.TrimSpace(tenant.Name) == "" {
		return fmt.Errorf("tenant code and name are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := tx.QueryRowContext(ctx, `
		INSERT INTO iam_tenants (code, name, status)
		VALUES ($1, $2, 'ACTIVE')
		RETURNING id, created_at, updated_at
	`, tenant.Code, tenant.Name).Scan(&tenant.ID, &tenant.CreatedAt, &tenant.UpdatedAt); err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO iam_organizations (code, name, status, tenant_id)
		VALUES ('root', $1, 'ACTIVE', $2)
	`, tenant.Name+" Root", tenant.ID); err != nil {
		return fmt.Errorf("create tenant root organization: %w", err)
	}
	if strings.TrimSpace(ownerUserID) != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO iam_tenant_memberships (tenant_id, user_id, status, is_default)
			VALUES ($1, $2, 'ACTIVE', false)
			ON CONFLICT (tenant_id, user_id) DO NOTHING
		`, tenant.ID, ownerUserID); err != nil {
			return fmt.Errorf("create tenant owner membership: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tenant transaction: %w", err)
	}
	return nil
}
