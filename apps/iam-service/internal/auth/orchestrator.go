package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/audit"
	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/hydra"
	"github.com/arda-labs/arda/apps/iam-service/internal/policy"
	"github.com/arda-labs/arda/apps/iam-service/internal/provider"
	"github.com/arda-labs/arda/apps/iam-service/internal/ratelimit"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

// Orchestrator coordinates the authentication flow between providers, Hydra, and the identity mapper.
type Orchestrator struct {
	registry        *provider.Registry
	hydra           *hydra.Client
	userRepo        *repository.UserRepository
	policyEnf       *policy.Enforcer
	limiter         *ratelimit.Limiter
	audit           *audit.Logger
	logger          *slog.Logger

	// Hydra client config
	hydraClientID string
	redirectURI   string
}

// NewOrchestrator creates a new auth orchestrator.
func NewOrchestrator(
	reg *provider.Registry,
	hydraClient *hydra.Client,
	userRepo *repository.UserRepository,
	policyEnf *policy.Enforcer,
	limiter *ratelimit.Limiter,
	audit *audit.Logger,
	hydraClientID string,
	redirectURI string,
) *Orchestrator {
	return &Orchestrator{
		registry:      reg,
		hydra:         hydraClient,
		userRepo:      userRepo,
		policyEnf:     policyEnf,
		limiter:       limiter,
		audit:         audit,
		logger:        slog.Default(),
		hydraClientID: hydraClientID,
		redirectURI:   redirectURI,
	}
}

// ===========================================================================
// Login page data
// ===========================================================================

// LoginPageData is returned to the login UI.
type LoginPageData struct {
	LoginChallenge string            `json:"login_challenge"`
	Providers      []provider.Metadata `json:"providers"`
	Hints          *LoginHints       `json:"hints,omitempty"`
	Error          string            `json:"error,omitempty"`
}

// LoginHints suggests a provider based on email or other hints.
type LoginHints struct {
	EmailHint         string `json:"email_hint,omitempty"`
	SuggestedProvider string `json:"suggested_provider,omitempty"`
}

// GetLoginPageData returns the data needed to render the login page.
func (o *Orchestrator) GetLoginPageData(ctx context.Context, loginChallenge string) (*LoginPageData, error) {
	// Fetch the login request from Hydra to get hints
	req, err := o.hydra.GetLoginRequest(ctx, loginChallenge)
	if err != nil {
		return nil, fmt.Errorf("get hydra login request: %w", err)
	}

	providers := o.registry.ListEnabled()

	var hints *LoginHints
	if req.OIDCContext.LoginHint != "" {
		if p, ok := o.registry.ResolveByEmail(req.OIDCContext.LoginHint); ok {
			hints = &LoginHints{
				EmailHint:         req.OIDCContext.LoginHint,
				SuggestedProvider: p.Metadata().ID,
			}
		}
	}

	return &LoginPageData{
		LoginChallenge: loginChallenge,
		Providers:      providers,
		Hints:          hints,
	}, nil
}

// ListProviders returns all enabled providers (for the login UI).
func (o *Orchestrator) ListProviders() []provider.Metadata {
	return o.registry.ListEnabled()
}

// ===========================================================================
// Password login
// ===========================================================================

// PasswordLoginRequest is sent from the login form.
type PasswordLoginRequest struct {
	LoginChallenge string `json:"login_challenge"`
	Username       string `json:"username"`
	Password       string `json:"password"`
}

// LoginCompleteResult is returned after a successful login.
type LoginCompleteResult struct {
	RedirectURL string       `json:"redirect_url"`
	User        *domain.UserContext `json:"user,omitempty"`
	PolicyCheck *PolicyCheckResult `json:"policy_check,omitempty"`
}

// PolicyCheckResult is returned after policy evaluation.
type PolicyCheckResult struct {
	Allowed bool   `json:"allowed"`
	Error   string `json:"error,omitempty"`
}

// CheckPolicy evaluates Casbin policy for a given context.
func (o *Orchestrator) CheckPolicy(ctx context.Context, sub, obj, act string, env map[string]any) (*PolicyCheckResult, error) {
	if o.policyEnf == nil {
		return &PolicyCheckResult{Allowed: true}, nil
	}

	allowed, err := o.policyEnf.Enforce(sub, obj, act, env)
	if err != nil {
		return &PolicyCheckResult{Allowed: false, Error: err.Error()}, err
	}

	if !allowed {
		o.audit.Event(ctx, &domain.AuthEvent{
			EventType: "permission_denied",
			Subject:   sub,
			Action:    act,
			Resource:  obj,
			Result:    "denied",
			Details:   env,
		})
	}

	return &PolicyCheckResult{Allowed: allowed}, nil
}

// LoginWithPassword authenticates a user with username/password.
func (o *Orchestrator) LoginWithPassword(ctx context.Context, req *PasswordLoginRequest, clientIP, userAgent, requestID string) (*LoginCompleteResult, error) {
	// 1. Rate limit check
	if err := o.limiter.CheckLogin(req.Username); err != nil {
		o.audit.LoginBlocked(ctx, req.Username, err.Error(), clientIP, userAgent, requestID)
		return nil, fmt.Errorf("rate limited: %w", err)
	}

	// 2. Get the internal provider
	p, err := o.registry.Get("internal")
	if err != nil {
		return nil, fmt.Errorf("internal provider not available: %w", err)
	}

	if !p.SupportsDirect() {
		return nil, fmt.Errorf("internal provider misconfigured")
	}

	o.logger.Info("login attempt",
		"username", req.Username,
		"client_ip", clientIP,
		"login_challenge", req.LoginChallenge != "",
	)

	// 3. Authenticate
	authResult, err := p.AuthenticateDirect(ctx, &provider.DirectAuthRequest{
		LoginChallenge: req.LoginChallenge,
		Credential: map[string]string{
			"username": req.Username,
			"password": req.Password,
		},
	})

	// 4. Record failure or reset
	if err != nil {
		o.limiter.RecordFailure(req.Username)
		o.audit.LoginAttempt(ctx, req.Username, false, "internal", err.Error(), clientIP, userAgent, requestID)
		o.logger.Warn("login failed", "username", req.Username, "reason", err.Error())
		return nil, err
	}
	o.logger.Info("login authenticated by provider", "username", req.Username, "user_id", authResult.InternalUserID)
	o.limiter.Reset(req.Username)
	o.audit.LoginAttempt(ctx, req.Username, true, "internal", "", clientIP, userAgent, requestID)

	// 5. Complete login + auto-accept consent in one shot
	result, err := o.finalizeLogin(ctx, req.LoginChallenge, authResult, clientIP, requestID)
	if err != nil {
		return nil, err
	}

	// After login is accepted, Hydra redirects to consent
	// We accept it immediately to avoid an extra redirect to the SPA
	if o.hydra != nil {
		o.logger.Info("auto-accepting consent after password login")
		// In a stateless flow, the SPA will handle consent on the redirect
		// This is normal — don't try to parse the consent challenge here
	}

	return result, nil
}

// ===========================================================================
// External login (SSO)
// ===========================================================================

// ExternalLoginRequest starts an SSO login with an external provider.
type ExternalLoginRequest struct {
	LoginChallenge string `json:"login_challenge"`
	ProviderID     string `json:"provider_id"`
	Hints          map[string]string `json:"hints"`
}

// InitiateExternalLogin starts an interactive login with an external IdP.
func (o *Orchestrator) InitiateExternalLogin(ctx context.Context, req *ExternalLoginRequest) (*provider.InitiateResponse, error) {
	p, err := o.registry.Get(req.ProviderID)
	if err != nil {
		return nil, err
	}

	if !p.SupportsInteractive() {
		return nil, fmt.Errorf("provider %q does not support interactive login", req.ProviderID)
	}

	// Build callback URL that the external IdP will redirect back to
	callbackURL := fmt.Sprintf("https://iam.arda.internal/api/auth/callback/%s", req.ProviderID)

	// Generate opaque state tying this to the Hydra login_challenge
	state := generateRandomString(32)
	// In production, store state → login_challenge mapping in Redis
	// For now, we embed the challenge in the state via a simple mechanism
	// The callback handler will verify

	initResp, err := p.InitiateAuthentication(ctx, &provider.InitiateRequest{
		LoginChallenge: req.LoginChallenge,
		RedirectURI:    callbackURL,
		State:          state,
		Hints:          req.Hints,
	})
	if err != nil {
		return nil, fmt.Errorf("initiate external auth: %w", err)
	}

	return initResp, nil
}

// HandleExternalCallback processes the OIDC/SAML callback from an external IdP.
func (o *Orchestrator) HandleExternalCallback(ctx context.Context, providerID string, queryParams map[string]string) (*LoginCompleteResult, error) {
	p, err := o.registry.Get(providerID)
	if err != nil {
		return nil, err
	}

	if !p.SupportsInteractive() {
		return nil, fmt.Errorf("provider %q does not support interactive login", providerID)
	}

	authResult, err := p.HandleCallback(ctx, &provider.CallbackRequest{
		State:       queryParams["state"],
		Code:        queryParams["code"],
		QueryParams: queryParams,
	})
	if err != nil {
		o.audit.LoginAttempt(ctx, queryParams["state"], false, providerID, err.Error(), "", "", "")
		return nil, fmt.Errorf("handle callback: %w", err)
	}

	// Identity mapping — look up internal user
	internalUser, err := o.mapIdentity(ctx, authResult)
	if err != nil {
		return nil, fmt.Errorf("map identity: %w", err)
	}
	authResult.InternalUserID = internalUser.ID

	// Get the login_challenge from state
	// In production: look up state → login_challenge in Redis
	loginChallenge := queryParams["login_challenge"]
	if loginChallenge == "" {
		loginChallenge = queryParams["state"] // fallback
	}

	o.audit.LoginAttempt(ctx, internalUser.Username, true, providerID, "", "", "", "")

	return o.finalizeLogin(ctx, loginChallenge, authResult, "", "")
}

// ===========================================================================
// Consent
// ===========================================================================

// HandleConsent processes a consent challenge. For internal clients, consent is auto-accepted.
// It extracts the user ID from the consent challenge itself.
func (o *Orchestrator) HandleConsent(ctx context.Context, consentChallenge string) (string, error) {
	req, err := o.hydra.GetConsentRequest(ctx, consentChallenge)
	if err != nil {
		return "", fmt.Errorf("get consent request: %w", err)
	}

	// Build session data using subject from consent challenge
	userCtx, err := o.buildUserContext(ctx, req.Subject)
	if err != nil {
		return "", fmt.Errorf("build user context: %w", err)
	}

	sessionData, _ := json.Marshal(userCtx)
	_ = sessionData

	// Auto-accept for all internal clients
	redirectTo, err := o.hydra.AcceptConsent(ctx, consentChallenge, &hydra.AcceptConsentBody{
		GrantScope: req.RequestedScope,
		Remember:   true,
		Session: &hydra.AcceptConsentSession{
			IDToken: map[string]any{
				"username":    userCtx.Username,
				"email":       userCtx.Email,
				"roles":       userCtx.Roles,
				"permissions": userCtx.Permissions,
			},
			AccessToken: map[string]any{
				"username":    userCtx.Username,
				"roles":       userCtx.Roles,
				"permissions": userCtx.Permissions,
				"org_ids":     userCtx.OrgIDs,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("accept consent: %w", err)
	}

	o.audit.ConsentGranted(ctx, req.Subject, req.Client.ClientID)
	return redirectTo, nil
}

// ===========================================================================
// Token exchange & refresh
// ===========================================================================

// TokenExchangeRequest is the callback from the SPA with the authorization code.
type TokenExchangeRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
	State        string `json:"state"`
}

// TokenResponse is returned after token exchange.
type TokenResponse struct {
	AccessToken  string            `json:"access_token"`
	RefreshToken string            `json:"refresh_token"`
	IDToken      string            `json:"id_token"`
	TokenType    string            `json:"token_type"`
	ExpiresIn    int               `json:"expires_in"`
	User         *domain.UserContext `json:"user"`
}

// ExchangeCode exchanges the authorization code for tokens.
func (o *Orchestrator) ExchangeCode(ctx context.Context, req *TokenExchangeRequest) (*TokenResponse, error) {
	redirectURI := o.redirectURI // must match the authorize request's redirect_uri

	tokenResp, err := o.hydra.ExchangeCode(ctx, req.Code, req.CodeVerifier, redirectURI, o.hydraClientID)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	// Decode ID token to get subject, then build user context
	var userCtx *domain.UserContext
	if tokenResp.IDToken != "" {
		if sub := extractSubjectFromIDToken(tokenResp.IDToken); sub != "" {
			if u, err := o.buildUserContext(ctx, sub); err == nil {
				userCtx = u
			}
		}
	}

	o.audit.TokenIssued(ctx, "user", "", "")

	return &TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
		User:         userCtx,
	}, nil
}

// RefreshToken exchanges a refresh token for new tokens.
func (o *Orchestrator) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	tokenResp, err := o.hydra.RefreshToken(ctx, refreshToken, o.hydraClientID)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	o.audit.TokenRefreshed(ctx, "user", "", "")

	return &TokenResponse{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken, // rotated
		IDToken:      tokenResp.IDToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    tokenResp.ExpiresIn,
	}, nil
}

// ===========================================================================
// Internal helpers
// ===========================================================================

// CreateUser creates a new internal user with a bcrypt-hashed password.
func (o *Orchestrator) CreateUser(ctx context.Context, username, email, password string) (*domain.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &domain.User{
		Username:     username,
		Email:        email,
		Subject:    username,
		DisplayName:  username,
		PasswordHash: string(hash),
		Source:       "internal",
		Status:       "ACTIVE",
		TenantID:     "default",
	}

	created, err := o.userRepo.CreateUser(ctx, user)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	o.logger.Info("user created", "username", username, "id", created.ID)
	return created, nil
}

func (o *Orchestrator) finalizeLogin(ctx context.Context, loginChallenge string, authResult *provider.AuthenticationResult, _, _ string) (*LoginCompleteResult, error) {
	// Load roles and permissions
	userCtx, err := o.buildUserContext(ctx, authResult.InternalUserID)
	if err != nil {
		return nil, fmt.Errorf("build user context: %w", err)
	}

	// Build accept login body
	acceptBody := &hydra.AcceptLoginBody{
		Subject:     authResult.InternalUserID,
		Remember:    true,
		RememberFor: 0,
		ACR:         authResult.ACR,
		AMR:         authResult.AMR,
		Context: map[string]any{
			"username":    userCtx.Username,
			"email":       userCtx.Email,
			"roles":       userCtx.Roles,
			"permissions": userCtx.Permissions,
			"org_ids":     userCtx.OrgIDs,
		},
	}

	// Accept login with Hydra
	redirectURL, err := o.hydra.AcceptLogin(ctx, loginChallenge, acceptBody)
	if err != nil {
		return nil, fmt.Errorf("hydra accept login: %w", err)
	}

	return &LoginCompleteResult{
		RedirectURL: redirectURL,
		User:        userCtx,
	}, nil
}

func (o *Orchestrator) buildUserContext(ctx context.Context, userID string) (*domain.UserContext, error) {
	user, err := o.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	roles, err := o.userRepo.GetUserRoles(ctx, userID)
	if err != nil {
		return nil, err
	}

	perms, err := o.userRepo.GetUserPermissions(ctx, userID)
	if err != nil {
		return nil, err
	}

	orgs, err := o.userRepo.GetUserOrganizations(ctx, userID)
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

func (o *Orchestrator) mapIdentity(ctx context.Context, result *provider.AuthenticationResult) (*domain.User, error) {
	// 1. Check existing mapping
	mapping, err := o.userRepo.FindIdentityMapping(ctx, result.ProviderID, result.ExternalID)
	if err == nil && mapping != nil && mapping.IsActive {
		u, err := o.userRepo.GetUserByID(ctx, mapping.InternalUserID)
		if err == nil && u != nil {
			return u, nil
		}
	}

	// 2. Try to match by email
	if email, ok := result.Claims["email"].(string); ok {
		u, err := o.userRepo.GetUserByEmail(ctx, email)
		if err == nil && u != nil {
			// Create mapping
			o.userRepo.CreateIdentityMapping(ctx, &domain.IdentityMapping{
				ProviderID:     result.ProviderID,
				ExternalID:     result.ExternalID,
				InternalUserID: u.ID,
				IsActive:       true,
				LastLoginAt:    time.Now(),
			})
			return u, nil
		}
	}

	// 3. Create new user (JIT provisioning)
	username := result.Claims["username"]
	if username == nil {
		username = result.ExternalID
	}
	email := result.Claims["email"]
	if email == nil {
		email = fmt.Sprintf("%s@%s.local", result.ExternalID, result.ProviderID)
	}

	newUser := &domain.User{
		Subject:    result.ExternalID,
		Username:   fmt.Sprintf("%s_%s", result.ProviderID, username),
		Email:      fmt.Sprintf("%v", email),
		Source:     result.ProviderID,
		Status:     "ACTIVE",
		TenantID:   "default",
	}

	created, err := o.userRepo.CreateUser(ctx, newUser)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	// Create identity mapping
	o.userRepo.CreateIdentityMapping(ctx, &domain.IdentityMapping{
		ProviderID:     result.ProviderID,
		ExternalID:     result.ExternalID,
		InternalUserID: created.ID,
		IsActive:       true,
		LastLoginAt:    time.Now(),
	})

	return created, nil
}

// extractSubjectFromIDToken parses the JWT payload of an ID token to extract the "sub" claim.
// No signature verification is needed here — Hydra already validated the token.
func extractSubjectFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.Sub
}

func generateRandomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}
