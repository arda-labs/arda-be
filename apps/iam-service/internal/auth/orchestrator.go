package auth

import (
	"context"
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

type LoginCompleteResult struct {
	RedirectURL string              `json:"redirect_url"`
	User        *domain.UserContext `json:"user,omitempty"`
	PolicyCheck *PolicyCheckResult  `json:"policy_check,omitempty"`
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
	ExpiresIn                                     int
	User                                          *domain.UserContext
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

func (o *Orchestrator) GetLoginPageData(ctx context.Context, loginChallenge string) (*LoginPageData, error) {
	return &LoginPageData{LoginChallenge: loginChallenge, Providers: o.registry.ListEnabled()}, nil
}
func (o *Orchestrator) ListProviders() []provider.Metadata { return o.registry.ListEnabled() }

func (o *Orchestrator) CheckPolicy(ctx context.Context, sub, obj, act string, env map[string]any) (*PolicyCheckResult, error) {
	if o.policyEnf == nil {
		return &PolicyCheckResult{Allowed: true}, nil
	}
	allowed, err := o.policyEnf.Enforce(sub, obj, act, env)
	if err != nil {
		return &PolicyCheckResult{Allowed: false, Error: err.Error()}, err
	}
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
