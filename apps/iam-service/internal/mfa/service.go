package mfa

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
)

// UserRepo defines the user repository methods needed by MFA.
type UserRepo interface {
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

// Service orchestrates MFA enrollment and verification.
type Service struct {
	totp     *TOTPService
	userRepo UserRepo
}

// NewService creates an MFA service.
func NewService(totp *TOTPService, userRepo UserRepo) *Service {
	return &Service{totp: totp, userRepo: userRepo}
}

// Enroll generates a new TOTP secret for a user.
func (s *Service) Enroll(ctx context.Context, userID, username, email string) (*TOTPSecret, error) {
	return s.totp.GenerateSecret(userID, username, email)
}

// Verify checks a TOTP code for a user and returns true if valid.
func (s *Service) Verify(ctx context.Context, userID, code string) (bool, error) {
	user, err := s.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("get user: %w", err)
	}
	if user == nil {
		return false, fmt.Errorf("user not found")
	}

	// In production, load the stored TOTP secret from a separate table
	// For now, this is a placeholder — the actual check uses the secret
	// stored in the user's MFA settings table
	secret := "" // load from "user_mfa_settings" table
	if secret == "" {
		return false, nil
	}

	return s.totp.Verify(secret, code)
}
