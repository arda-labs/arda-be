package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/kratos"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

const kratosProviderID = "kratos"

// IdentityService is the single IAM boundary for Kratos identity operations.
type IdentityService struct {
	userRepo *repository.UserRepository
	kratos   *kratos.Client
}

func NewIdentityService(userRepo *repository.UserRepository, kratosClient *kratos.Client) *IdentityService {
	return &IdentityService{userRepo: userRepo, kratos: kratosClient}
}

func (s *IdentityService) CreateIdentity(ctx context.Context, email, password, name string) (string, error) {
	if s.kratos == nil {
		return "", fmt.Errorf("kratos client is not configured")
	}
	identity, err := s.kratos.CreateIdentity(email, password, name)
	if err != nil {
		return "", err
	}
	return identity.ID, nil
}

func (s *IdentityService) LinkIdentity(ctx context.Context, user *domain.User, identityID string) error {
	if user == nil || user.ID == "" || strings.TrimSpace(identityID) == "" {
		return nil
	}
	user.KratosIdentityID = strings.TrimSpace(identityID)
	user.Subject = strings.TrimSpace(identityID)
	user.Source = kratosProviderID
	if err := s.userRepo.UpdateUser(ctx, user); err != nil {
		return err
	}
	return s.userRepo.CreateIdentityMapping(ctx, &domain.IdentityMapping{
		ProviderID:     kratosProviderID,
		ExternalID:     user.KratosIdentityID,
		InternalUserID: user.ID,
		IsActive:       true,
	})
}

func (s *IdentityService) ResolveKratosIdentityID(ctx context.Context, user *domain.User) (string, error) {
	if user == nil {
		return "", fmt.Errorf("user is required")
	}
	if identityID := strings.TrimSpace(user.KratosIdentityID); identityID != "" {
		return identityID, nil
	}
	mapping, err := s.userRepo.FindIdentityMappingByUser(ctx, kratosProviderID, user.ID)
	if err != nil {
		return "", fmt.Errorf("resolve kratos identity mapping: %w", err)
	}
	if mapping != nil && strings.TrimSpace(mapping.ExternalID) != "" {
		return strings.TrimSpace(mapping.ExternalID), nil
	}
	if strings.EqualFold(strings.TrimSpace(user.Source), kratosProviderID) && strings.TrimSpace(user.Subject) != "" {
		return strings.TrimSpace(user.Subject), nil
	}
	return "", fmt.Errorf("missing kratos identity id for user")
}

func (s *IdentityService) UpdateEmail(ctx context.Context, user *domain.User, newEmail string) (*domain.User, error) {
	if s.kratos == nil {
		return nil, fmt.Errorf("kratos client is not configured")
	}
	identityID, err := s.ResolveKratosIdentityID(ctx, user)
	if err != nil {
		return nil, err
	}
	if err := s.kratos.UpdateIdentityEmail(identityID, newEmail, user.DisplayName); err != nil {
		return nil, fmt.Errorf("update kratos identity email: %w", err)
	}
	updated, err := s.userRepo.UpdateUserEmail(ctx, user.ID, newEmail)
	if err != nil {
		return nil, fmt.Errorf("sync user email cache: %w", err)
	}
	if updated != nil && updated.KratosIdentityID == "" {
		updated.KratosIdentityID = identityID
		_ = s.userRepo.UpdateUser(ctx, updated)
	}
	return updated, nil
}

func (s *IdentityService) UpdatePassword(ctx context.Context, user *domain.User, newPassword string) error {
	if s.kratos == nil {
		return fmt.Errorf("kratos client is not configured")
	}
	if strings.TrimSpace(newPassword) == "" {
		return fmt.Errorf("password is required")
	}
	identityID, err := s.ResolveKratosIdentityID(ctx, user)
	if err != nil {
		return err
	}
	return s.kratos.UpdateIdentityPassword(identityID, newPassword)
}

func (s *IdentityService) ProvisionIdentity(ctx context.Context, user *domain.User, temporaryPassword string) (string, error) {
	if user == nil {
		return "", fmt.Errorf("user is required")
	}
	if strings.TrimSpace(temporaryPassword) == "" {
		return "", fmt.Errorf("temporary password is required")
	}
	if identityID := strings.TrimSpace(user.KratosIdentityID); identityID != "" {
		return identityID, nil
	}
	identityID, err := s.CreateIdentity(ctx, user.Email, temporaryPassword, user.DisplayName)
	createdIdentity := err == nil
	if err != nil {
		existing, findErr := s.kratos.FindIdentityByIdentifier(user.Email)
		if findErr != nil || existing == nil {
			return "", err
		}
		identityID = existing.ID
	}
	if err := s.LinkIdentity(ctx, user, identityID); err != nil {
		if createdIdentity {
			_ = s.kratos.DeleteIdentity(identityID)
		}
		return "", err
	}
	return identityID, nil
}

func (s *IdentityService) DeleteIdentity(ctx context.Context, user *domain.User) error {
	if s.kratos == nil || user == nil {
		return nil
	}
	identityID, err := s.ResolveKratosIdentityID(ctx, user)
	if err != nil {
		return nil
	}
	return s.kratos.DeleteIdentity(identityID)
}
