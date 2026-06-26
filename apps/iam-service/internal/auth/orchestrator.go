package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"

	"github.com/arda-labs/arda/apps/iam-service/internal/audit"
	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/hydra"
	"github.com/arda-labs/arda/apps/iam-service/internal/policy"
	"github.com/arda-labs/arda/apps/iam-service/internal/provider"
	"github.com/arda-labs/arda/apps/iam-service/internal/ratelimit"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"github.com/arda-labs/arda/apps/iam-service/internal/service"
	"golang.org/x/crypto/bcrypt"
)

type Orchestrator struct {
	registry      *provider.Registry
	hydra         *hydra.Client
	userRepo      *repository.UserRepository
	policyEnf     *policy.Enforcer
	limiter       *ratelimit.Limiter
	audit         *audit.Logger
	mfaSvc        *service.MFAService
	logger        *slog.Logger
	hydraClientID string
	redirectURI   string
}

func NewOrchestrator(
	reg *provider.Registry, hydraClient *hydra.Client, userRepo *repository.UserRepository,
	policyEnf *policy.Enforcer, limiter *ratelimit.Limiter, aud *audit.Logger,
	hydraClientID string, redirectURI string,
) *Orchestrator {
	return &Orchestrator{
		registry: reg, hydra: hydraClient, userRepo: userRepo, policyEnf: policyEnf,
		limiter: limiter, audit: aud, logger: slog.Default(),
		hydraClientID: hydraClientID, redirectURI: redirectURI,
	}
}

func (o *Orchestrator) WithMFAService(mfaSvc *service.MFAService) *Orchestrator {
	o.mfaSvc = mfaSvc
	return o
}

type PasswordLoginRequest struct {
	LoginChallenge string `json:"login_challenge"`
	Username       string `json:"username"`
	Password       string `json:"password"`
}

type LoginCompleteResult struct {
	RedirectURL string              `json:"redirect_url"`
	User        *domain.UserContext `json:"user,omitempty"`
	PolicyCheck *PolicyCheckResult  `json:"policy_check,omitempty"`
	RequiresMFA bool                `json:"requiresMfa,omitempty"`
	MFAToken    string              `json:"mfaToken,omitempty"`
	MFAUserID   string              `json:"mfaUserId,omitempty"`
}

type ExternalLoginRequest struct {
	LoginChallenge string            `json:"login_challenge"`
	ProviderID     string            `json:"provider_id"`
	Hints          map[string]string `json:"hints"`
}

type TokenExchangeRequest struct {
	Code, CodeVerifier, State string
}

type TokenResponse struct {
	AccessToken, RefreshToken, IDToken, TokenType string
	ExpiresIn int
	User      *domain.UserContext
}

type LoginPageData struct {
	LoginChallenge string              `json:"login_challenge"`
	Providers      []provider.Metadata `json:"providers"`
	Hints          *LoginHints         `json:"hints,omitempty"`
	Error          string              `json:"error,omitempty"`
}

type LoginHints struct {
	EmailHint         string `json:"email_hint,omitempty"`
	SuggestedProvider string `json:"suggested_provider,omitempty"`
}

type PolicyCheckResult struct {
	Allowed bool   `json:"allowed"`
	Error   string `json:"error,omitempty"`
}

func (o *Orchestrator) LoginWithPassword(ctx context.Context, req *PasswordLoginRequest, clientIP, userAgent, requestID string) (*LoginCompleteResult, error) {
	if err := o.limiter.CheckLogin(req.Username); err != nil {
		return nil, fmt.Errorf("rate limited: %w", err)
	}
	p, err := o.registry.Get("internal")
	if err != nil { return nil, fmt.Errorf("internal provider not available: %w", err) }
	if !p.SupportsDirect() { return nil, fmt.Errorf("internal provider misconfigured") }
	authResult, err := p.AuthenticateDirect(ctx, &provider.DirectAuthRequest{
		Credential: map[string]string{"username": req.Username, "password": req.Password},
	})
	if err != nil {
		o.limiter.RecordFailure(req.Username)
		o.audit.LoginAttempt(ctx, req.Username, false, "internal", err.Error(), clientIP, userAgent, requestID)
		return nil, err
	}
	o.limiter.Reset(req.Username)
	o.audit.LoginAttempt(ctx, req.Username, true, "internal", "", clientIP, userAgent, requestID)
	return o.finalizeLogin(ctx, req.LoginChallenge, authResult)
}

func (o *Orchestrator) finalizeLogin(ctx context.Context, loginChallenge string, authResult *provider.AuthenticationResult) (*LoginCompleteResult, error) {
	user, err := o.userRepo.GetUserByID(ctx, authResult.InternalUserID)
	if err != nil || user == nil { return nil, fmt.Errorf("user not found") }
	userCtx := &domain.UserContext{
		UserID: user.ID, Subject: user.Subject, Username: user.Username, Email: user.Email, TenantID: user.TenantID,
	}
	if loginChallenge == "" {
		return &LoginCompleteResult{User: userCtx}, nil
	}
	redirectURL, err := o.hydra.AcceptLogin(ctx, loginChallenge, &hydra.AcceptLoginBody{
		Subject: authResult.InternalUserID, Remember: true,
	})
	if err != nil { return nil, fmt.Errorf("hydra accept login: %w", err) }
	return &LoginCompleteResult{RedirectURL: redirectURL, User: userCtx}, nil
}

func (o *Orchestrator) GetLoginPageData(ctx context.Context, loginChallenge string) (*LoginPageData, error) {
	return &LoginPageData{LoginChallenge: loginChallenge, Providers: o.registry.ListEnabled()}, nil
}
func (o *Orchestrator) ListProviders() []provider.Metadata { return o.registry.ListEnabled() }

func (o *Orchestrator) CheckPolicy(ctx context.Context, sub, obj, act string, env map[string]any) (*PolicyCheckResult, error) {
	if o.policyEnf == nil { return &PolicyCheckResult{Allowed: true}, nil }
	allowed, err := o.policyEnf.Enforce(sub, obj, act, env)
	if err != nil { return &PolicyCheckResult{Allowed: false, Error: err.Error()}, err }
	return &PolicyCheckResult{Allowed: allowed}, nil
}

func (o *Orchestrator) InitiateExternalLogin(ctx context.Context, req *ExternalLoginRequest) (*provider.InitiateResponse, error) {
	return nil, fmt.Errorf("external login not configured")
}
func (o *Orchestrator) HandleExternalCallback(ctx context.Context, providerID string, queryParams map[string]string) (*LoginCompleteResult, error) {
	return nil, fmt.Errorf("external login not configured")
}
func (o *Orchestrator) ExchangeCode(ctx context.Context, req *TokenExchangeRequest) (*TokenResponse, error) {
	return nil, fmt.Errorf("token exchange not configured")
}
func (o *Orchestrator) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	return nil, fmt.Errorf("refresh not configured")
}
func (o *Orchestrator) HandleConsent(ctx context.Context, consentChallenge string) (string, error) {
	return "", fmt.Errorf("consent not configured")
}

func (o *Orchestrator) CreateUser(ctx context.Context, username, email, password string) (*domain.User, error) {
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	user := &domain.User{
		Username: username, Email: email, Subject: username,
		PasswordHash: string(hash), Source: "internal", Status: "ACTIVE", TenantID: "default",
	}
	return o.userRepo.CreateUser(ctx, user)
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}
