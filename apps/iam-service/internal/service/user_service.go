package service

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

// UserService orchestrates user-related business logic.
type UserService struct {
	repo *repository.UserRepository
}

// NewUserService creates a new user service.
func NewUserService(repo *repository.UserRepository) *UserService {
	return &UserService{repo: repo}
}

// GetUserContextBySubject builds a user context from an external subject.
func (s *UserService) GetUserContextBySubject(ctx context.Context, subject string) (*domain.UserContext, error) {
	user, err := s.repo.GetUserBySubject(ctx, subject)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

// GetUserContextByID builds a user context from a user UUID.
func (s *UserService) GetUserContextByID(ctx context.Context, id string) (*domain.UserContext, error) {
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

func (s *UserService) buildContext(ctx context.Context, user *domain.User) (*domain.UserContext, error) {
	roles, err := s.repo.GetUserRoles(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	perms, err := s.repo.GetUserPermissions(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	orgs, err := s.repo.GetUserOrganizations(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	roleCodes := make([]string, len(roles))
	for i, r := range roles {
		roleCodes[i] = r.Code
	}

	permCodes := make([]string, len(perms))
	for i, p := range perms {
		permCodes[i] = p.Code
	}

	return &domain.UserContext{
		UserID:      user.ID,
		Subject:     user.Subject,
		Username:    user.Username,
		Email:       user.Email,
		TenantID:    user.TenantID,
		OrgIDs:      orgs,
		Roles:       roleCodes,
		Permissions: permCodes,
	}, nil
}
