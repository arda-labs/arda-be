package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

// RoleRepository provides persistence for roles and permissions.
type RoleRepository struct {
	db *sql.DB
}

// NewRoleRepository creates a role repository.
func NewRoleRepository(db *sql.DB) *RoleRepository {
	return &RoleRepository{db: db}
}

// ── Roles ──

type ListRolesParams struct {
	Page     int
	Size     int
	TenantID string
	Search   string
}

func (r *RoleRepository) Create(ctx context.Context, role *domain.Role) error {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO iam_roles (code, name, status, tenant_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`, role.Code, role.Name, "ACTIVE", role.TenantID)
	return row.Scan(&role.ID, &role.CreatedAt, &role.UpdatedAt)
}

func (r *RoleRepository) GetByID(ctx context.Context, id string) (*domain.Role, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM iam_roles WHERE id = $1
	`, id)
	return scanRole(row)
}

func (r *RoleRepository) GetByCode(ctx context.Context, code string) (*domain.Role, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, code, name, status, created_at, updated_at
		FROM iam_roles WHERE code = $1
	`, code)
	return scanRole(row)
}

func (r *RoleRepository) List(ctx context.Context, params ListRolesParams) ([]domain.Role, int, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1

	if params.TenantID != "" {
		where = append(where, fmt.Sprintf("(tenant_id = $%d OR tenant_id = 'default')", idx))
		args = append(args, params.TenantID)
		idx++
	}
	if params.Search != "" {
		where = append(where, fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d)", idx, idx))
		args = append(args, "%"+params.Search+"%")
		idx++
	}

	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM iam_roles WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (params.Page - 1) * params.Size
	query := fmt.Sprintf(`
		SELECT id, code, name, status, created_at, updated_at
		FROM iam_roles WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d
	`, wc, idx, idx+1)
	allArgs := append(args, params.Size, offset)

	rows, err := r.db.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var roles []domain.Role
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Status, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, 0, err
		}
		roles = append(roles, role)
	}
	return roles, total, rows.Err()
}

func (r *RoleRepository) Update(ctx context.Context, role *domain.Role) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iam_roles SET name = $1, updated_at = now() WHERE id = $2
	`, role.Name, role.ID)
	return err
}

func (r *RoleRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_roles WHERE id = $1`, id)
	return err
}

// ── Permissions ──

type ListPermissionsParams struct {
	Page     int
	Size     int
	Module   string
	TenantID string
}

func (r *RoleRepository) CreatePermission(ctx context.Context, p *domain.Permission) error {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO iam_permissions (code, name, module_code, resource_code, operation_code)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, p.Code, p.Name, p.Module, p.Resource, p.Operation)
	return row.Scan(&p.ID, &p.CreatedAt)
}

func (r *RoleRepository) GetPermissionByID(ctx context.Context, id string) (*domain.Permission, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, code, name, module_code, resource_code, operation_code, created_at
		FROM iam_permissions WHERE id = $1
	`, id)
	return scanPermission(row)
}

func (r *RoleRepository) ListPermissions(ctx context.Context, params ListPermissionsParams) ([]domain.Permission, int, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1

	if params.Module != "" {
		where = append(where, fmt.Sprintf("module_code = $%d", idx))
		args = append(args, params.Module)
		idx++
	}

	wc := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM iam_permissions WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (params.Page - 1) * params.Size
	query := fmt.Sprintf(`
		SELECT id, code, name, module_code, resource_code, operation_code, created_at
		FROM iam_permissions WHERE %s ORDER BY module_code, resource_code LIMIT $%d OFFSET $%d
	`, wc, idx, idx+1)
	allArgs := append(args, params.Size, offset)

	rows, err := r.db.QueryContext(ctx, query, allArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var perms []domain.Permission
	for rows.Next() {
		var p domain.Permission
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Module, &p.Resource, &p.Operation, &p.CreatedAt); err != nil {
			return nil, 0, err
		}
		perms = append(perms, p)
	}
	return perms, total, rows.Err()
}

func (r *RoleRepository) DeletePermission(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_permissions WHERE id = $1`, id)
	return err
}

// ── Role-Permission mapping ──

func (r *RoleRepository) AssignPermission(ctx context.Context, roleID, permID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_role_permissions (role_id, permission_id)
		VALUES ($1, $2) ON CONFLICT DO NOTHING
	`, roleID, permID)
	return err
}

func (r *RoleRepository) UnassignPermission(ctx context.Context, roleID, permID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_role_permissions WHERE role_id = $1 AND permission_id = $2`, roleID, permID)
	return err
}

func (r *RoleRepository) ListPermissionsByRole(ctx context.Context, roleID string) ([]domain.Permission, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.code, p.name, p.module_code, p.resource_code, p.operation_code, p.created_at
		FROM iam_permissions p
		JOIN iam_role_permissions rp ON rp.permission_id = p.id
		WHERE rp.role_id = $1
	`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []domain.Permission
	for rows.Next() {
		var p domain.Permission
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Module, &p.Resource, &p.Operation, &p.CreatedAt); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// ── Scanners ──

func scanRole(row *sql.Row) (*domain.Role, error) {
	role := &domain.Role{}
	err := row.Scan(&role.ID, &role.Code, &role.Name, &role.Status, &role.CreatedAt, &role.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan role: %w", err)
	}
	return role, nil
}

func scanPermission(row *sql.Row) (*domain.Permission, error) {
	p := &domain.Permission{}
	err := row.Scan(&p.ID, &p.Code, &p.Name, &p.Module, &p.Resource, &p.Operation, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan permission: %w", err)
	}
	return p, nil
}
