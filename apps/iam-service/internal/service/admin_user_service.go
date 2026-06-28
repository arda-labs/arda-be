package service

import (
	"context"
	"fmt"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

type AdminUserSummary struct {
	ID        string
	Username  string
	Email     string
	Name      string
	Status    string
	Roles     []string
	TenantID  string
	CreatedAt time.Time
}

type AdminUserDetails struct {
	User  *domain.User
	Roles []string
}

type CreateAdminUserInput struct {
	Username string
	Email    string
	Password string
	Name     string
	TenantID string
	RoleIDs  []string
}

type UpdateAdminUserInput struct {
	Username *string
	Email    *string
	Name     *string
	Status   *string
	TenantID *string
}

type AdminUserService struct {
	userRepo *repository.UserRepository
	roleRepo *repository.RoleRepository
	identity *IdentityService
}

func NewAdminUserService(userRepo *repository.UserRepository, roleRepo *repository.RoleRepository, identity *IdentityService) *AdminUserService {
	return &AdminUserService{
		userRepo: userRepo,
		roleRepo: roleRepo,
		identity: identity,
	}
}

func (s *AdminUserService) ListUsers(ctx context.Context, params repository.ListUsersParams) ([]AdminUserSummary, int, error) {
	users, total, err := s.userRepo.ListUsers(ctx, params)
	if err != nil {
		return nil, 0, err
	}

	items := make([]AdminUserSummary, 0, len(users))
	for _, u := range users {
		roles, err := s.userRepo.GetUserRoles(ctx, u.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("get user roles: %w", err)
		}
		roleCodes := make([]string, len(roles))
		for i, r := range roles {
			roleCodes[i] = r.Code
		}
		items = append(items, AdminUserSummary{
			ID:        u.ID,
			Username:  u.Username,
			Email:     u.Email,
			Name:      u.DisplayName,
			Status:    u.Status,
			Roles:     roleCodes,
			TenantID:  u.TenantID,
			CreatedAt: u.CreatedAt,
		})
	}

	return items, total, nil
}

func (s *AdminUserService) GetUser(ctx context.Context, id string) (*AdminUserDetails, error) {
	user, err := s.userRepo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	roles, err := s.userRepo.GetUserRoles(ctx, id)
	if err != nil {
		return nil, err
	}
	roleCodes := make([]string, len(roles))
	for i, r := range roles {
		roleCodes[i] = r.Code
	}

	return &AdminUserDetails{
		User:  user,
		Roles: roleCodes,
	}, nil
}

func (s *AdminUserService) CreateUser(ctx context.Context, input CreateAdminUserInput) (*domain.User, error) {
	kratosIdentityID, err := s.identity.CreateIdentity(ctx, input.Email, input.Password, input.Name)
	if err != nil {
		return nil, err
	}

	user := &domain.User{
		Username:         input.Username,
		Email:            input.Email,
		DisplayName:      input.Name,
		Subject:          kratosIdentityID,
		KratosIdentityID: kratosIdentityID,
		Source:           kratosProviderID,
		Status:           "ACTIVE",
		TenantID:         input.TenantID,
	}

	created, err := s.userRepo.CreateUser(ctx, user)
	if err != nil {
		_ = s.identity.DeleteIdentity(ctx, user)
		return nil, err
	}

	if err := s.userRepo.CreateIdentityMapping(ctx, &domain.IdentityMapping{
		ProviderID:     kratosProviderID,
		ExternalID:     kratosIdentityID,
		InternalUserID: created.ID,
		IsActive:       true,
	}); err != nil {
		return nil, err
	}

	for _, roleID := range input.RoleIDs {
		if err := s.userRepo.AssignRole(ctx, created.ID, roleID); err != nil {
			return nil, err
		}
	}

	return created, nil
}

func (s *AdminUserService) UpdateUser(ctx context.Context, id string, input UpdateAdminUserInput) (*domain.User, error) {
	user, err := s.userRepo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	if input.Username != nil {
		user.Username = *input.Username
	}
	if input.Email != nil {
		if user.Source == kratosProviderID {
			updated, err := s.identity.UpdateEmail(ctx, user, *input.Email)
			if err != nil {
				return nil, err
			}
			user = updated
		} else {
			user.Email = *input.Email
		}
	}
	if input.Name != nil {
		user.DisplayName = *input.Name
	}
	if input.Status != nil {
		user.Status = *input.Status
	}
	if input.TenantID != nil {
		user.TenantID = *input.TenantID
	}

	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AdminUserService) DeleteUser(ctx context.Context, id string) (*domain.User, error) {
	user, err := s.userRepo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	if user.Source == kratosProviderID {
		_ = s.identity.DeleteIdentity(ctx, user)
	}
	if err := s.userRepo.DeleteUser(ctx, id); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AdminUserService) SetStatus(ctx context.Context, id, status string) (*domain.User, error) {
	user, err := s.userRepo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	user.Status = status
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AdminUserService) ResetPassword(ctx context.Context, id, newPassword string) (*domain.User, error) {
	user, err := s.userRepo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}
	if user.Source != kratosProviderID {
		return nil, fmt.Errorf("password is not managed by Kratos for this user")
	}
	if err := s.identity.UpdatePassword(ctx, user, newPassword); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *AdminUserService) ProvisionIdentity(ctx context.Context, id, temporaryPassword string) (*domain.User, string, error) {
	user, err := s.userRepo.GetUserByID(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if user == nil {
		return nil, "", nil
	}
	identityID, err := s.identity.ProvisionIdentity(ctx, user, temporaryPassword)
	if err != nil {
		return nil, "", err
	}
	updated, err := s.userRepo.GetUserByID(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return updated, identityID, nil
}

func (s *AdminUserService) AuditIdentityConsistency(ctx context.Context) ([]repository.IdentityConsistencyIssue, error) {
	return s.userRepo.AuditKratosIdentityConsistency(ctx)
}
