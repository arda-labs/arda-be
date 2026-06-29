package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

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

// ListUsersParams for paginated user listing.
type ListUsersParams struct {
	Page      int
	Size      int
	Status    string
	Search    string // search by username or email
	TenantID  string
	SortField string
	SortOrder string
}

type IdentityConsistencyIssue struct {
	Type              string `json:"type"`
	UserID            string `json:"userId,omitempty"`
	Username          string `json:"username,omitempty"`
	Email             string `json:"email,omitempty"`
	KratosIdentityID  string `json:"kratosIdentityId,omitempty"`
	MappingIdentityID string `json:"mappingIdentityId,omitempty"`
	Count             int    `json:"count,omitempty"`
}

// scanUserRow scans a user row into a User.
func scanUserRow(scanner interface {
	Scan(dest ...any) error
}, u *domain.User) error {
	return scanner.Scan(&u.ID, &u.Subject, &u.KratosIdentityID, &u.Username, &u.Email, &u.DisplayName, &u.Nickname,
		&u.FirstName, &u.LastName, &u.PhoneNumber, &u.Birthdate, &u.Gender, &u.Address, &u.Country,
		&u.Source, &u.Status, &u.TenantID, &u.AvatarFileID, &u.PictureURL, &u.CoverFileID, &u.CoverImageURL,
		&u.Department, &u.Position, &u.EmployeeID, &u.ApprovalLevel, &u.DailyLimit, &u.Bio,
		&u.CreatedAt, &u.UpdatedAt)
}

// ListUsers returns paginated users with total count.
func (r *UserRepository) ListUsers(ctx context.Context, params ListUsersParams) ([]domain.User, int, error) {
	where := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if params.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, params.Status)
		argIdx++
	}
	if params.TenantID != "" {
		where = append(where, fmt.Sprintf("tenant_id = $%d", argIdx))
		args = append(args, params.TenantID)
		argIdx++
	}
	if params.Search != "" {
		where = append(where, fmt.Sprintf("(username ILIKE $%d OR email ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+params.Search+"%")
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	// Count total
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM iam_users WHERE %s", whereClause)
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	// Fetch page
	offset := (params.Page - 1) * params.Size

	sortCol := "created_at"
	if params.SortField != "" {
		switch params.SortField {
		case "username":
			sortCol = "username"
		case "email":
			sortCol = "email"
		case "status":
			sortCol = "status"
		case "createdAt":
			sortCol = "created_at"
		}
	}
	sortDir := "DESC"
	if strings.ToUpper(params.SortOrder) == "ASC" {
		sortDir = "ASC"
	}

	query := fmt.Sprintf(`
		SELECT id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		       COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		       COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		       COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		       COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		       COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		       created_at, updated_at
		FROM iam_users
		WHERE %s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, whereClause, sortCol, sortDir, argIdx, argIdx+1)
	args = append(args, params.Size, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		var u domain.User
		if err := scanUserRow(rows, &u); err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

// UpdateUser updates a user record.
func (r *UserRepository) UpdateUser(ctx context.Context, u *domain.User) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iam_users SET
			username = $1, email = $2, display_name = $3,
			nickname = $4, status = $5, tenant_id = $6,
			department = $7, position = $8, employee_id = $9,
			approval_level = $10, daily_limit = $11, bio = $12,
			first_name = $13, last_name = $14,
			phone_number = $15, birthdate = $16, gender = $17, address = $18, country = $19,
			cover_file_id = $20, cover_image_url = $21,
			kratos_identity_id = NULLIF($22, ''),
			updated_at = now()
		WHERE id = $23
	`, u.Username, u.Email, u.DisplayName, u.Nickname, u.Status, u.TenantID,
		u.Department, u.Position, u.EmployeeID, u.ApprovalLevel, u.DailyLimit, u.Bio,
		u.FirstName, u.LastName, u.PhoneNumber, u.Birthdate, u.Gender, u.Address, u.Country,
		u.CoverFileID, u.CoverImageURL, u.KratosIdentityID, u.ID)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (r *UserRepository) UpdateUserAvatar(ctx context.Context, userID, avatarFileID, pictureURL string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE iam_users
		SET avatar_file_id = NULLIF($2, ''),
		    picture_url = NULLIF($3, ''),
		    updated_at = now()
		WHERE id = $1
		RETURNING id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		          COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		          COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		          COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		          COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		          COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		          created_at, updated_at
	`, userID, avatarFileID, pictureURL)
	u := &domain.User{}
	if err := scanUserRow(row, u); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update user avatar: %w", err)
	}
	return u, nil
}

func (r *UserRepository) UpdateUserCover(ctx context.Context, userID, coverFileID, coverImageURL string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE iam_users
		SET cover_file_id = NULLIF($2, ''),
		    cover_image_url = NULLIF($3, ''),
		    updated_at = now()
		WHERE id = $1
		RETURNING id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		          COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		          COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		          COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		          COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		          COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		          created_at, updated_at
	`, userID, coverFileID, coverImageURL)
	u := &domain.User{}
	if err := scanUserRow(row, u); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update user cover: %w", err)
	}
	return u, nil
}

func (r *UserRepository) UpdateUserProfile(ctx context.Context, userID, name, nickname, firstName, lastName, phoneNumber, birthdate, gender, address, country, position, department, employeeID, approvalLevel, dailyLimit, bio string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE iam_users
		SET display_name = $2,
		    nickname = $3,
		    first_name = $4,
		    last_name = $5,
		    phone_number = $6,
		    birthdate = $7,
		    gender = $8,
		    address = $9,
		    country = $10,
		    position = $11,
		    department = $12,
		    employee_id = $13,
		    approval_level = $14,
		    daily_limit = $15,
		    bio = $16,
		    updated_at = now()
		WHERE id = $1
		RETURNING id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		          COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		          COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		          COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		          COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		          COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		          created_at, updated_at
	`, userID, name, nickname, firstName, lastName, phoneNumber, birthdate, gender, address, country, position, department, employeeID, approvalLevel, dailyLimit, bio)
	u := &domain.User{}
	if err := scanUserRow(row, u); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update user profile: %w", err)
	}
	return u, nil
}

func (r *UserRepository) UpdateUserEmail(ctx context.Context, userID, email string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE iam_users
		SET email = $2,
		    updated_at = now()
		WHERE id = $1
		RETURNING id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		          COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		          COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		          COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		          COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		          COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		          created_at, updated_at
	`, userID, email)
	u := &domain.User{}
	if err := scanUserRow(row, u); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update user email in db: %w", err)
	}
	return u, nil
}

// DeleteUser permanently removes a user.
func (r *UserRepository) DeleteUser(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM iam_users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// AssignRole assigns a role to a user.
func (r *UserRepository) AssignRole(ctx context.Context, userID, roleID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO iam_user_roles (user_id, role_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, userID, roleID)
	return err
}

// UnassignRole removes a role from a user.
func (r *UserRepository) UnassignRole(ctx context.Context, userID, roleID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM iam_user_roles WHERE user_id = $1 AND role_id = $2
	`, userID, roleID)
	return err
}

func (r *UserRepository) UserHasRoleCode(ctx context.Context, userID, roleCode string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM iam_user_roles ur
			JOIN iam_roles r ON r.id = ur.role_id
			WHERE ur.user_id = $1 AND r.code = $2
		)
	`, userID, roleCode).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check user role: %w", err)
	}
	return exists, nil
}

func (r *UserRepository) CountActiveUsersWithRoleCode(ctx context.Context, roleCode string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM iam_users u
		JOIN iam_user_roles ur ON ur.user_id = u.id
		JOIN iam_roles r ON r.id = ur.role_id
		WHERE r.code = $1 AND u.status = 'ACTIVE'
	`, roleCode).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active users by role: %w", err)
	}
	return count, nil
}

// ── Existing methods below ──

// GetUserBySubject loads a user by external subject (OIDC sub).
func (r *UserRepository) GetUserBySubject(ctx context.Context, subject string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		       COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		       COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		       COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		       COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		       COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		       created_at, updated_at
		FROM iam_users
		WHERE external_subject = $1
	`, subject)

	return r.scanUser(row)
}

// GetUserByID loads a user by UUID.
func (r *UserRepository) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		       COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		       COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		       COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		       COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		       COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		       created_at, updated_at
		FROM iam_users
		WHERE id = $1
	`, id)

	return r.scanUser(row)
}

func (r *UserRepository) GetUserByKratosIdentityID(ctx context.Context, identityID string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		       COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		       COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		       COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		       COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		       COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		       created_at, updated_at
		FROM iam_users
		WHERE kratos_identity_id = $1
	`, identityID)

	return r.scanUser(row)
}

// GetUserByUsername loads a user by username (for password auth).
func (r *UserRepository) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		       COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		       COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		       COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		       COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		       COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		       created_at, updated_at
		FROM iam_users
		WHERE username = $1
	`, username)

	return r.scanUser(row)
}

// GetUserByEmail loads a user by email (for identity mapping).
func (r *UserRepository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, external_subject, COALESCE(kratos_identity_id,''), username, email, display_name,
		       COALESCE(nickname,''), COALESCE(first_name,''), COALESCE(last_name,''),
		       COALESCE(phone_number,''), COALESCE(birthdate,''), COALESCE(gender,''), COALESCE(address,''), COALESCE(country,''),
		       COALESCE(source,'internal'), status, tenant_id,
		       COALESCE(avatar_file_id,''), COALESCE(picture_url,''), COALESCE(cover_file_id,''), COALESCE(cover_image_url,''),
		       COALESCE(department,''), COALESCE(position,''), COALESCE(employee_id,''),
		       COALESCE(approval_level,''), COALESCE(daily_limit,''), COALESCE(bio,''),
		       created_at, updated_at
		FROM iam_users
		WHERE email = $1
	`, email)

	return r.scanUser(row)
}

// CreateUser inserts a new user.
func (r *UserRepository) CreateUser(ctx context.Context, u *domain.User) (*domain.User, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO iam_users (
			external_subject, kratos_identity_id, username, email, display_name,
			nickname, first_name, last_name, gender, address, country, position,
			source, status, tenant_id
		)
		VALUES (
			$1, NULLIF($2, ''), $3, $4, $5,
			$6, $7, $8, $9, $10, $11, $12,
			$13, $14, $15
		)
		RETURNING id, created_at, updated_at
	`, u.Subject, u.KratosIdentityID, u.Username, u.Email, u.DisplayName,
		u.Nickname, u.FirstName, u.LastName, u.Gender, u.Address, u.Country, u.Position,
		u.Source, u.Status, u.TenantID)

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

// FindIdentityMappingByUser looks up an identity mapping by provider + internal user ID.
func (r *UserRepository) FindIdentityMappingByUser(ctx context.Context, providerID, internalUserID string) (*domain.IdentityMapping, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, provider_id, external_id, internal_user_id, is_active, last_login_at, created_at
		FROM iam_identity_mappings
		WHERE provider_id = $1 AND internal_user_id = $2
		ORDER BY created_at ASC
		LIMIT 1
	`, providerID, internalUserID)

	m := &domain.IdentityMapping{}
	err := row.Scan(&m.ID, &m.ProviderID, &m.ExternalID, &m.InternalUserID, &m.IsActive, &m.LastLoginAt, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find identity mapping by user: %w", err)
	}
	return m, nil
}

func (r *UserRepository) AuditKratosIdentityConsistency(ctx context.Context) ([]IdentityConsistencyIssue, error) {
	var issues []IdentityConsistencyIssue

	missingRows, err := r.db.QueryContext(ctx, `
		SELECT id, username, email
		FROM iam_users
		WHERE source = 'kratos' AND COALESCE(kratos_identity_id, '') = ''
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("audit missing kratos identity id: %w", err)
	}
	defer missingRows.Close()
	for missingRows.Next() {
		var issue IdentityConsistencyIssue
		issue.Type = "missing_kratos_identity_id"
		if err := missingRows.Scan(&issue.UserID, &issue.Username, &issue.Email); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	if err := missingRows.Err(); err != nil {
		return nil, err
	}

	duplicateRows, err := r.db.QueryContext(ctx, `
		SELECT kratos_identity_id, COUNT(*)
		FROM iam_users
		WHERE COALESCE(kratos_identity_id, '') <> ''
		GROUP BY kratos_identity_id
		HAVING COUNT(*) > 1
	`)
	if err != nil {
		return nil, fmt.Errorf("audit duplicate kratos identity id: %w", err)
	}
	defer duplicateRows.Close()
	for duplicateRows.Next() {
		var issue IdentityConsistencyIssue
		issue.Type = "duplicate_kratos_identity_id"
		if err := duplicateRows.Scan(&issue.KratosIdentityID, &issue.Count); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	if err := duplicateRows.Err(); err != nil {
		return nil, err
	}

	mismatchRows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, COALESCE(u.kratos_identity_id, ''), m.external_id
		FROM iam_users u
		JOIN iam_identity_mappings m ON m.internal_user_id = u.id AND m.provider_id = 'kratos'
		WHERE COALESCE(u.kratos_identity_id, '') <> ''
		  AND COALESCE(m.external_id, '') <> ''
		  AND u.kratos_identity_id <> m.external_id
	`)
	if err != nil {
		return nil, fmt.Errorf("audit kratos mapping mismatch: %w", err)
	}
	defer mismatchRows.Close()
	for mismatchRows.Next() {
		var issue IdentityConsistencyIssue
		issue.Type = "kratos_mapping_mismatch"
		if err := mismatchRows.Scan(&issue.UserID, &issue.Username, &issue.Email, &issue.KratosIdentityID, &issue.MappingIdentityID); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	if err := mismatchRows.Err(); err != nil {
		return nil, err
	}

	return issues, nil
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
	err := scanUserRow(row, u)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}
