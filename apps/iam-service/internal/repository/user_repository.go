package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

// UserRepository provides persistence for users and their context.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new repository backed by db.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// GetUserBySubject loads a user by external subject (OIDC sub).
func (r *UserRepository) GetUserBySubject(ctx context.Context, subject string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, username, email, display_name, COALESCE(password_hash,''), COALESCE(source,'internal'), status, tenant_id, created_at, updated_at
		FROM iam_users
		WHERE external_subject = $1
	`, subject)

	return r.scanUser(row)
}

// GetUserByID loads a user by UUID.
func (r *UserRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, username, email, display_name, COALESCE(password_hash,''), COALESCE(source,'internal'), status, tenant_id, created_at, updated_at
		FROM iam_users
		WHERE id = $1
	`, id)

	return r.scanUser(row)
}

// GetUserByUsername loads a user by username (for password auth).
func (r *UserRepository) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, username, email, display_name, COALESCE(password_hash,''), COALESCE(source,'internal'), status, tenant_id, created_at, updated_at
		FROM iam_users
		WHERE username = $1
	`, username)

	return r.scanUser(row)
}

// GetUserByEmail loads a user by email (for identity mapping).
func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, username, email, display_name, COALESCE(password_hash,''), COALESCE(source,'internal'), status, tenant_id, created_at, updated_at
		FROM iam_users
		WHERE email = $1
	`, email)

	return r.scanUser(row)
}

// CreateUser inserts a new user.
func (r *UserRepository) CreateUser(ctx context.Context, u *domain.User) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO iam_users (external_subject, username, email, display_name, password_hash, source, status, tenant_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at
	`, u.Subject, u.Username, u.Email, u.DisplayName, u.PasswordHash, u.Source, u.Status, u.TenantID)

	err := row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// CreateIdentityMapping links an external identity to an internal user.
func (r *UserRepository) CreateIdentityMapping(ctx context.Context, m *domain.IdentityMapping) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_identity_mappings (provider_id, external_id, internal_user_id, is_active, last_login_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (provider_id, external_id) DO UPDATE
		SET last_login_at = now(), is_active = $4
	`, m.ProviderID, m.ExternalID, m.InternalUserID, m.IsActive)
	if err != nil {
		return fmt.Errorf("create identity mapping: %w", err)
	}
	return nil
}

// FindIdentityMapping looks up an identity mapping by provider + external ID.
func (r *UserRepository) FindIdentityMapping(ctx context.Context, providerID, externalID string) (*domain.IdentityMapping, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, provider_id, external_id, internal_user_id, is_active, last_login_at, created_at
		FROM iam_identity_mappings
		WHERE provider_id = $1 AND external_id = $2
	`, providerID, externalID)

	m := &domain.IdentityMapping{}
	err := row.Scan(&m.ID, &m.ProviderID, &m.ExternalID, &m.InternalUserID, &m.IsActive, &m.LastLoginAt, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find identity mapping: %w", err)
	}
	return m, nil
}

// GetUserRoles returns all roles assigned to a user.
func (r *UserRepository) GetUserRoles(ctx context.Context, userID string) ([]domain.Role, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT r.id, r.code, r.name
		FROM iam_roles r
		JOIN iam_user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user roles: %w", err)
	}
	defer rows.Close()

	var roles []domain.Role
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role.ID, &role.Code, &role.Name); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// GetUserPermissions returns all permissions granted to a user through their roles.
func (r *UserRepository) GetUserPermissions(ctx context.Context, userID string) ([]domain.Permission, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.code, p.name, p.module_code, p.resource_code, p.operation_code
		FROM iam_permissions p
		JOIN iam_role_permissions rp ON rp.permission_id = p.id
		JOIN iam_user_roles ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = $1
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user permissions: %w", err)
	}
	defer rows.Close()

	var perms []domain.Permission
	for rows.Next() {
		var p domain.Permission
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Module, &p.Resource, &p.Operation); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// GetUserOrganizations returns organization codes a user belongs to.
func (r *UserRepository) GetUserOrganizations(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT o.code
		FROM iam_organizations o
		JOIN iam_user_organizations uo ON uo.organization_id = o.id
		WHERE uo.user_id = $1
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user orgs: %w", err)
	}
	defer rows.Close()

	var orgs []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		orgs = append(orgs, code)
	}
	return orgs, rows.Err()
}

// InsertAuditLog writes an audit event to the database.
func (r *UserRepository) InsertAuditLog(ctx context.Context, e *domain.AuthEvent) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_audit_logs (event_type, subject, action, resource, result, details, client_ip, user_agent, request_id, service_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, e.EventType, e.Subject, e.Action, e.Resource, e.Result,
		marshalDetails(e.Details),
		e.ClientIP, e.UserAgent, e.RequestID, e.ServiceName)
	if err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

func marshalDetails(d map[string]any) any {
	if d == nil {
		return nil
	}
	b, _ := json.Marshal(d)
	return string(b)
}

func (r *UserRepository) scanUser(row *sql.Row) (*domain.User, error) {
	u := &domain.User{}
	err := row.Scan(&u.ID, &u.Subject, &u.Username, &u.Email, &u.DisplayName,
		&u.PasswordHash, &u.Source, &u.Status, &u.TenantID, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}
