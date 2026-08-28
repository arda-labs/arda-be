package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

type GroupRepository struct {
	db *sql.DB
}

func NewGroupRepository(db *sql.DB) *GroupRepository {
	return &GroupRepository{db: db}
}

type ListGroupsParams struct {
	Page     int
	Size     int
	TenantID string
	Status   string
	Search   string
}

func (r *GroupRepository) List(ctx context.Context, params ListGroupsParams) ([]domain.Group, int, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if params.TenantID != "" {
		where = append(where, fmt.Sprintf("g.tenant_id = $%d", idx))
		args = append(args, params.TenantID)
		idx++
	}
	if params.Status != "" {
		where = append(where, fmt.Sprintf("g.status = $%d", idx))
		args = append(args, params.Status)
		idx++
	}
	if params.Search != "" {
		where = append(where, fmt.Sprintf("(g.code ILIKE $%d OR g.name ILIKE $%d)", idx, idx))
		args = append(args, "%"+params.Search+"%")
		idx++
	}

	wc := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM iam_groups g WHERE "+wc, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count groups: %w", err)
	}

	offset := (params.Page - 1) * params.Size
	query := fmt.Sprintf(`
		SELECT g.id, g.code, g.name, COALESCE(g.description, ''), g.status, g.tenant_id, g.is_system,
		       COUNT(DISTINCT gm.user_id), COUNT(DISTINCT ra.role_id), g.created_at, g.updated_at
		FROM iam_groups g
		LEFT JOIN iam_group_members gm ON gm.group_id = g.id
		LEFT JOIN iam_role_assignments ra ON ra.principal_type = 'GROUP' AND ra.principal_id = g.id
		WHERE %s
		GROUP BY g.id
		ORDER BY g.created_at DESC
		LIMIT $%d OFFSET $%d
	`, wc, idx, idx+1)
	rows, err := r.db.QueryContext(ctx, query, append(args, params.Size, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	groups := []domain.Group{}
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.Code, &g.Name, &g.Description, &g.Status, &g.TenantID, &g.IsSystem, &g.MemberCount, &g.RoleCount, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, 0, err
		}
		groups = append(groups, g)
	}
	return groups, total, rows.Err()
}

func (r *GroupRepository) StreamGroups(ctx context.Context, params ListGroupsParams) (*sql.Rows, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if params.TenantID != "" {
		where = append(where, fmt.Sprintf("g.tenant_id = $%d", idx))
		args = append(args, params.TenantID)
		idx++
	}
	if params.Status != "" {
		where = append(where, fmt.Sprintf("g.status = $%d", idx))
		args = append(args, params.Status)
		idx++
	}
	if params.Search != "" {
		where = append(where, fmt.Sprintf("(g.code ILIKE $%d OR g.name ILIKE $%d)", idx, idx))
		args = append(args, "%"+params.Search+"%")
		idx++
	}

	wc := strings.Join(where, " AND ")
	query := fmt.Sprintf(`
		SELECT g.id, g.code, g.name, COALESCE(g.description, ''), g.status,
		       COUNT(DISTINCT gm.user_id), COUNT(DISTINCT ra.role_id), g.created_at
		FROM iam_groups g
		LEFT JOIN iam_group_members gm ON gm.group_id = g.id
		LEFT JOIN iam_role_assignments ra ON ra.principal_type = 'GROUP' AND ra.principal_id = g.id
		WHERE %s
		GROUP BY g.id
		ORDER BY g.created_at DESC
	`, wc)
	return r.db.QueryContext(ctx, query, args...)
}

func (r *GroupRepository) GetByIDScoped(ctx context.Context, id, tenantID string) (*domain.Group, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT g.id, g.code, g.name, COALESCE(g.description, ''), g.status, g.tenant_id, g.is_system,
		       COUNT(DISTINCT gm.user_id), COUNT(DISTINCT ra.role_id), g.created_at, g.updated_at
		FROM iam_groups g
		LEFT JOIN iam_group_members gm ON gm.group_id = g.id
		LEFT JOIN iam_role_assignments ra ON ra.principal_type = 'GROUP' AND ra.principal_id = g.id
		WHERE g.id = $1 AND g.tenant_id = $2
		GROUP BY g.id
	`, id, tenantID)
	return scanGroup(row)
}

func (r *GroupRepository) Create(ctx context.Context, group *domain.Group) error {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO iam_groups (code, name, description, status, tenant_id, is_system)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, group.Code, group.Name, group.Description, group.Status, group.TenantID, group.IsSystem)
	return row.Scan(&group.ID, &group.CreatedAt, &group.UpdatedAt)
}

func (r *GroupRepository) UpdateScoped(ctx context.Context, group *domain.Group, tenantID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iam_groups
		SET name = $2,
		    description = NULLIF($3, ''),
		    status = $4,
		    tenant_id = $5,
		    updated_at = now()
		WHERE id = $1 AND tenant_id = $6
	`, group.ID, group.Name, group.Description, group.Status, group.TenantID, tenantID)
	return err
}

func (r *GroupRepository) DeleteScoped(ctx context.Context, id, tenantID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_groups WHERE id = $1 AND tenant_id = $2 AND is_system = false`, id, tenantID)
	return err
}

func (r *GroupRepository) ListMembersScoped(ctx context.Context, groupID, tenantID string) ([]domain.User, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.external_subject, COALESCE(u.kratos_identity_id,''), u.username, u.email, u.display_name,
		       COALESCE(u.nickname,''), COALESCE(u.first_name,''), COALESCE(u.last_name,''),
		       COALESCE(u.phone_number,''), COALESCE(u.birthdate,''), COALESCE(u.gender,''), COALESCE(u.address,''), COALESCE(u.country,''),
		       COALESCE(u.source,'internal'), u.status, u.tenant_id,
		       COALESCE(u.avatar_file_id,''), COALESCE(u.picture_url,''), COALESCE(u.cover_file_id,''), COALESCE(u.cover_image_url,''),
		       COALESCE(u.department,''), COALESCE(u.position,''), COALESCE(u.employee_id,''),
		       COALESCE(u.approval_level,''), COALESCE(u.daily_limit,''), COALESCE(u.bio,''),
		       u.created_at, u.updated_at
		FROM iam_users u
		JOIN iam_group_members gm ON gm.user_id = u.id
		JOIN iam_groups g ON g.id = gm.group_id
		WHERE gm.group_id = $1 AND g.tenant_id = $2 AND u.tenant_id = $2
		ORDER BY u.display_name, u.username
	`, groupID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list group members: %w", err)
	}
	defer rows.Close()

	users := []domain.User{}
	for rows.Next() {
		var u domain.User
		if err := scanUserRow(rows, &u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *GroupRepository) AddMemberScoped(ctx context.Context, groupID, userID, tenantID, createdBy string) error {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_group_members (group_id, user_id, created_by)
		SELECT g.id, u.id, NULLIF($4, '')::uuid
		FROM iam_groups g
		JOIN iam_users u ON u.id = $2 AND u.tenant_id = $3
		WHERE g.id = $1 AND g.tenant_id = $3
		ON CONFLICT DO NOTHING
	`, groupID, userID, tenantID, createdBy)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return fmt.Errorf("group or user is outside target tenant")
	}
	return nil
}

func (r *GroupRepository) RemoveMemberScoped(ctx context.Context, groupID, userID, tenantID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM iam_group_members gm
		USING iam_groups g, iam_users u
		WHERE gm.group_id = g.id AND gm.user_id = u.id
		  AND g.id = $1 AND u.id = $2 AND g.tenant_id = $3 AND u.tenant_id = $3
	`, groupID, userID, tenantID)
	return err
}

func (r *GroupRepository) ListUserGroups(ctx context.Context, userID string) ([]domain.Group, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT g.id, g.code, g.name, COALESCE(g.description, ''), g.status, g.tenant_id, g.is_system,
		       COUNT(DISTINCT gm2.user_id), COUNT(DISTINCT ra.role_id), g.created_at, g.updated_at
		FROM iam_groups g
		JOIN iam_group_members gm ON gm.group_id = g.id
		LEFT JOIN iam_group_members gm2 ON gm2.group_id = g.id
		LEFT JOIN iam_role_assignments ra ON ra.principal_type = 'GROUP' AND ra.principal_id = g.id
		WHERE gm.user_id = $1
		GROUP BY g.id
		ORDER BY g.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user groups: %w", err)
	}
	defer rows.Close()

	groups := []domain.Group{}
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.Code, &g.Name, &g.Description, &g.Status, &g.TenantID, &g.IsSystem, &g.MemberCount, &g.RoleCount, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (r *GroupRepository) ListUserGroupsScoped(ctx context.Context, userID, tenantID string) ([]domain.Group, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT g.id, g.code, g.name, COALESCE(g.description, ''), g.status, g.tenant_id, g.is_system,
		       COUNT(DISTINCT gm2.user_id), COUNT(DISTINCT ra.role_id), g.created_at, g.updated_at
		FROM iam_groups g
		JOIN iam_group_members gm ON gm.group_id = g.id
		LEFT JOIN iam_group_members gm2 ON gm2.group_id = g.id
		LEFT JOIN iam_role_assignments ra ON ra.principal_type = 'GROUP' AND ra.principal_id = g.id
		WHERE gm.user_id = $1 AND g.tenant_id = $2
		GROUP BY g.id
		ORDER BY g.name
	`, userID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list scoped user groups: %w", err)
	}
	defer rows.Close()

	groups := []domain.Group{}
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.Code, &g.Name, &g.Description, &g.Status, &g.TenantID, &g.IsSystem, &g.MemberCount, &g.RoleCount, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (r *GroupRepository) ListRolesScoped(ctx context.Context, groupID, tenantID string) ([]domain.Role, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, r.code, r.name, r.status, COALESCE(r.tenant_id, ''), r.created_at, r.updated_at
		FROM iam_roles r
		JOIN iam_role_assignments ra ON ra.role_id = r.id
		WHERE ra.principal_type = 'GROUP'
		  AND ra.principal_id = $1
		  AND r.tenant_id = $2
		  AND ra.status = 'ACTIVE'
		ORDER BY r.code
	`, groupID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list group roles: %w", err)
	}
	defer rows.Close()

	roles := []domain.Role{}
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Status, &role.TenantID, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (r *GroupRepository) AssignRoleScoped(ctx context.Context, groupID, roleID, tenantID, createdBy string) error {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_role_assignments (principal_type, principal_id, role_id, scope_type, created_by)
		SELECT 'GROUP', g.id, ro.id, 'global', NULLIF($4, '')::uuid
		FROM iam_groups g
		JOIN iam_roles ro ON ro.id = $2 AND ro.tenant_id = $3
		WHERE g.id = $1 AND g.tenant_id = $3
		ON CONFLICT DO NOTHING
	`, groupID, roleID, tenantID, createdBy)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return fmt.Errorf("group or role is outside target tenant")
	}
	return nil
}

func (r *GroupRepository) UnassignRoleScoped(ctx context.Context, groupID, roleID, tenantID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM iam_role_assignments ra
		USING iam_groups g, iam_roles ro
		WHERE ra.principal_type = 'GROUP'
		  AND ra.principal_id = g.id
		  AND ra.role_id = ro.id
		  AND g.id = $1 AND ro.id = $2
		  AND g.tenant_id = $3
		  AND ro.tenant_id = $3
		  AND ra.scope_type = 'global'
		  AND ra.scope_id IS NULL
	`, groupID, roleID, tenantID)
	return err
}

func scanGroup(row *sql.Row) (*domain.Group, error) {
	group := &domain.Group{}
	err := row.Scan(&group.ID, &group.Code, &group.Name, &group.Description, &group.Status, &group.TenantID, &group.IsSystem, &group.MemberCount, &group.RoleCount, &group.CreatedAt, &group.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan group: %w", err)
	}
	return group, nil
}
