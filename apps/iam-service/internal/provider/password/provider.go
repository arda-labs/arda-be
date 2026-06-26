package password

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/provider"
	"golang.org/x/crypto/bcrypt"
)

// UserStore is the interface for user persistence needed by the password provider.
type UserStore interface {
	GetUserByUsername(ctx context.Context, username string) (*domain.User, error)
}

// PasswordProvider authenticates users with username/password.
type PasswordProvider struct {
	userStore UserStore
}

// New creates a new password provider.
func New(store UserStore) *PasswordProvider {
	return &PasswordProvider{userStore: store}
}

// Metadata returns the provider metadata.
func (p *PasswordProvider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          "internal",
		Type:        provider.TypePassword,
		Name:        "ARDA Account",
		Description: "Đăng nhập bằng tài khoản nội bộ",
		Priority:    0,
		IsPrimary:   true,
		IsEnabled:   true,
		Domains:     []string{"arda.io.vn"},
	}
}

// Validate checks the provider can operate.
func (p *PasswordProvider) Validate(ctx context.Context) error {
	if p.userStore == nil {
		return fmt.Errorf("user store is nil")
	}
	return nil
}

// SupportsInteractive returns false — password auth is direct.
func (p *PasswordProvider) SupportsInteractive() bool { return false }

// InitiateAuthentication is not supported for password provider.
func (p *PasswordProvider) InitiateAuthentication(ctx context.Context, req *provider.InitiateRequest) (*provider.InitiateResponse, error) {
	return nil, fmt.Errorf("password provider does not support interactive auth")
}

// HandleCallback is not supported for password provider.
func (p *PasswordProvider) HandleCallback(ctx context.Context, req *provider.CallbackRequest) (*provider.AuthenticationResult, error) {
	return nil, fmt.Errorf("password provider does not support callbacks")
}

// SupportsDirect returns true — password auth accepts credentials directly.
func (p *PasswordProvider) SupportsDirect() bool { return true }

// AuthenticateDirect verifies username/password and returns a unified result.
func (p *PasswordProvider) AuthenticateDirect(ctx context.Context, req *provider.DirectAuthRequest) (*provider.AuthenticationResult, error) {
	username := req.Credential["username"]
	password := req.Credential["password"]

	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password required")
	}

	slog.Debug("password auth attempt", "username", username)

	u, err := p.userStore.GetUserByUsername(ctx, username)
	if err != nil {
		slog.Warn("get user error", "username", username, "err", err)
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		slog.Warn("user not found", "username", username)
		return nil, fmt.Errorf("invalid credentials")
	}

	slog.Debug("user found", "username", username, "status", u.Status, "source", u.Source, "hash_len", len(u.PasswordHash))

	if u.Status != "ACTIVE" {
		slog.Warn("account not active", "username", username, "status", u.Status)
		return nil, fmt.Errorf("account is %s", u.Status)
	}

	if u.Source != "" && u.Source != "internal" {
		slog.Warn("wrong provider", "username", username, "source", u.Source)
		return nil, fmt.Errorf("account uses external identity provider: %s", u.Source)
	}

	if u.PasswordHash == "" {
		slog.Warn("empty password hash", "username", username)
		return nil, fmt.Errorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		slog.Warn("password mismatch", "username", username, "err", err)
		return nil, fmt.Errorf("invalid credentials")
	}

	slog.Info("password auth success", "username", username)

	return &provider.AuthenticationResult{
		ExternalID:     u.ID,
		ProviderID:     "internal",
		ProviderType:   provider.TypePassword,
		InternalUserID: u.ID,
		ACR:            "password",
		AMR:            []string{"password"},
		AuthTime:       time.Now(),
		SessionData: &provider.SessionData{
			Subject: u.ID,
			IDTokenExtra: map[string]any{
				"username": u.Username,
				"email":    u.Email,
			},
		},
		Claims: map[string]any{
			"sub":      u.ID,
			"username": u.Username,
			"email":    u.Email,
		},
	}, nil
}
