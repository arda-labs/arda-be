package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/iam-service/internal/provider"
)

// OIDCConfig holds configuration for an OIDC identity provider.
type OIDCConfig struct {
	ProviderID   string   `yaml:"id"`
	ProviderName string   `yaml:"name"`
	IssuerURL    string   `yaml:"issuer_url"`
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	Scopes       []string `yaml:"scopes"`
	Domains      []string `yaml:"domains"`
	ButtonColor  string   `yaml:"button_color"`
	IconURL      string   `yaml:"icon_url"`
	Priority     int      `yaml:"priority"`
}

// OIDCProvider authenticates users via OpenID Connect (Entra ID, Google, Keycloak).
type OIDCProvider struct {
	config     OIDCConfig
	httpClient *http.Client
}

// New creates an OIDC provider.
func New(cfg OIDCConfig) *OIDCProvider {
	return &OIDCProvider{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (p *OIDCProvider) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          p.config.ProviderID,
		Type:        provider.TypeOIDC,
		Name:        p.config.ProviderName,
		Description: fmt.Sprintf("Đăng nhập bằng %s", p.config.ProviderName),
		ButtonColor: p.config.ButtonColor,
		IconURL:     p.config.IconURL,
		Priority:    p.config.Priority,
		IsEnabled:   true,
		Domains:     p.config.Domains,
	}
}

func (p *OIDCProvider) Validate(ctx context.Context) error {
	if p.config.IssuerURL == "" || p.config.ClientID == "" {
		return fmt.Errorf("oidc %q: issuer_url and client_id required", p.config.ProviderID)
	}
	// Fetch OpenID configuration to validate the issuer
	discURL := strings.TrimSuffix(p.config.IssuerURL, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, discURL, nil)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("oidc %q: cannot fetch discovery: %w", p.config.ProviderID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc %q: discovery returned %d", p.config.ProviderID, resp.StatusCode)
	}
	return nil
}

func (p *OIDCProvider) SupportsInteractive() bool { return true }
func (p *OIDCProvider) SupportsDirect() bool      { return false }

func (p *OIDCProvider) InitiateAuthentication(ctx context.Context, req *provider.InitiateRequest) (*provider.InitiateResponse, error) {
	scopes := strings.Join(p.config.Scopes, " ")
	if scopes == "" {
		scopes = "openid profile email"
	}

	// Generate PKCE challenge
	codeVerifier := generateCodeVerifier()
	codeChallenge := sha256Base64(codeVerifier)

	// Build authorize URL
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", p.config.ClientID)
	params.Set("redirect_uri", req.RedirectURI)
	params.Set("scope", scopes)
	params.Set("state", req.State)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")

	authURL := fmt.Sprintf("%s/authorize?%s", strings.TrimSuffix(p.config.IssuerURL, "/"), params.Encode())

	return &provider.InitiateResponse{
		RedirectURL: authURL,
		Data: map[string]string{
			"code_verifier": codeVerifier,
		},
	}, nil
}

func (p *OIDCProvider) HandleCallback(ctx context.Context, req *provider.CallbackRequest) (*provider.AuthenticationResult, error) {
	code := req.Code
	if code == "" {
		code = req.QueryParams["code"]
	}

	// Exchange code for token
	tokenURL := fmt.Sprintf("%s/token", strings.TrimSuffix(p.config.IssuerURL, "/"))
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", req.QueryParams["redirect_uri"])
	data.Set("client_id", p.config.ClientID)
	data.Set("client_secret", p.config.ClientSecret)
	data.Set("code_verifier", req.QueryParams["code_verifier"])
	if data.Get("code_verifier") == "" {
		data.Set("code_verifier", req.FormParams["code_verifier"])
	}

	tokenReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	tokenResp, err := p.httpClient.Do(tokenReq)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		return nil, fmt.Errorf("token exchange failed: HTTP %d: %s", tokenResp.StatusCode, string(body))
	}

	var tokenData struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		Scope       string `json:"scope"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	// Decode ID token (JWT) to get claims
	// We parse only the payload part (no signature verification needed here)
	claims, err := p.decodeIDToken(tokenData.IDToken)
	if err != nil {
		return nil, fmt.Errorf("decode id_token: %w", err)
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, fmt.Errorf("id_token missing subject")
	}

	return &provider.AuthenticationResult{
		ExternalID:   sub,
		ProviderID:   p.config.ProviderID,
		ProviderType: provider.TypeOIDC,
		Claims:       claims,
		ACR:          "sso",
		AMR:          []string{"oidc"},
		AuthTime:     time.Now(),
		SessionData: &provider.SessionData{
			Subject: sub,
			IDTokenExtra: map[string]any{
				"idp": p.config.ProviderID,
				"acr": "sso",
				"amr": []string{"oidc"},
			},
		},
	}, nil
}

func (p *OIDCProvider) AuthenticateDirect(ctx context.Context, req *provider.DirectAuthRequest) (*provider.AuthenticationResult, error) {
	return nil, fmt.Errorf("oidc does not support direct auth")
}

// decodeIDToken decodes the JWT payload (no signature verification).
func (p *OIDCProvider) decodeIDToken(idToken string) (map[string]any, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT: expected 3 parts, got %d", len(parts))
	}

	// Decode payload (parts[1])
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Try with padding
		payload, err = base64.RawStdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("decode JWT payload: %w", err)
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("parse JWT claims: %w", err)
	}
	return claims, nil
}

// ── PKCE helpers ──

func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func sha256Base64(input string) string {
	h := sha256.Sum256([]byte(input))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
