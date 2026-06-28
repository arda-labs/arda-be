package service

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/kratos"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

// UserService orchestrates user-related business logic.
type UserService struct {
	repo   *repository.UserRepository
	kratos *kratos.Client
}

// NewUserService creates a new user service.
func NewUserService(repo *repository.UserRepository, kratos *kratos.Client) *UserService {
	return &UserService{repo: repo, kratos: kratos}
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

func (s *UserService) UpdateUserAvatar(ctx context.Context, userID, avatarFileID, pictureURL string) (*domain.UserContext, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if avatarFileID == "" && pictureURL == "" {
		return nil, fmt.Errorf("avatar_file_id or picture_url is required")
	}
	user, err := s.repo.UpdateUserAvatar(ctx, userID, avatarFileID, pictureURL)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

func (s *UserService) UpdateUserCover(ctx context.Context, userID, coverFileID, coverImageURL string) (*domain.UserContext, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if coverFileID == "" && coverImageURL == "" {
		return nil, fmt.Errorf("cover_file_id or cover_image_url is required")
	}
	user, err := s.repo.UpdateUserCover(ctx, userID, coverFileID, coverImageURL)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

func (s *UserService) UpdateUserProfile(ctx context.Context, userID, name, firstName, lastName, phoneNumber, birthdate, gender, address, country, position, department, employeeID, approvalLevel, dailyLimit, bio string) (*domain.UserContext, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	user, err := s.repo.UpdateUserProfile(ctx, userID, name, firstName, lastName, phoneNumber, birthdate, gender, address, country, position, department, employeeID, approvalLevel, dailyLimit, bio)
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
		UserID:        user.ID,
		Subject:       user.Subject,
		Username:      user.Username,
		Email:         user.Email,
		DisplayName:   user.DisplayName,
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		PhoneNumber:   user.PhoneNumber,
		Birthdate:     user.Birthdate,
		Gender:        user.Gender,
		Address:       user.Address,
		Country:       user.Country,
		PictureURL:    user.PictureURL,
		AvatarFileID:  user.AvatarFileID,
		CoverImageURL: user.CoverImageURL,
		CoverFileID:   user.CoverFileID,
		TenantID:      user.TenantID,
		OrgIDs:        orgs,
		Roles:         roleCodes,
		Permissions:   permCodes,
		Department:    user.Department,
		Position:      user.Position,
		EmployeeID:    user.EmployeeID,
		ApprovalLevel: user.ApprovalLevel,
		DailyLimit:    user.DailyLimit,
		Bio:           user.Bio,
	}, nil
}

func (s *UserService) UpdateUserEmail(ctx context.Context, userID, newEmail string) (*domain.UserContext, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if newEmail == "" {
		return nil, fmt.Errorf("email is required")
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	if user.Email == newEmail {
		return s.buildContext(ctx, user)
	}

	if user.Subject != "" {
		err = s.kratos.UpdateIdentityEmail(user.Subject, newEmail, user.DisplayName)
		if err != nil {
			return nil, fmt.Errorf("failed to update Kratos identity: %w", err)
		}
	}

	updatedUser, err := s.repo.UpdateUserEmail(ctx, userID, newEmail)
	if err != nil {
		return nil, fmt.Errorf("failed to update user email in DB: %w", err)
	}
	if updatedUser == nil {
		return nil, fmt.Errorf("user not found after update")
	}

	return s.buildContext(ctx, updatedUser)
}
