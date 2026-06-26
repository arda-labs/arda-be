package hydra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is an HTTP client for Hydra Admin API and Public API.
type Client struct {
	adminURL  string
	publicURL string
	client    *http.Client
}

// New creates a new Hydra client.
func New(adminURL, publicURL string) *Client {
	return &Client{
		adminURL:  adminURL,
		publicURL: publicURL,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// --- Admin API types ---

// LoginRequest is the Hydra login challenge response.
type LoginRequest struct {
	Challenge string `json:"challenge"`
	Skip      bool   `json:"skip"`
	Subject   string `json:"subject"`
	Client    struct {
		ClientID string `json:"client_id"`
	} `json:"client"`
	RequestURL string `json:"request_url"`
	RequestedScope []string `json:"requested_scope"`
	OIDCContext struct {
		LoginHint   string `json:"login_hint"`
		ACRValues   []string `json:"acr_values"`
	} `json:"oidc_context"`
}

// AcceptLoginBody is the body sent to accept a login request.
type AcceptLoginBody struct {
	Subject     string                 `json:"subject"`
	Remember    bool                   `json:"remember"`
	RememberFor int                    `json:"remember_for"`
	ACR         string                 `json:"acr,omitempty"`
	AMR         []string               `json:"amr,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
}

// AcceptLoginResponse is the response from accepting login.
type AcceptLoginResponse struct {
	RedirectTo string `json:"redirect_to"`
}

// ConsentRequest is the Hydra consent challenge response.
type ConsentRequest struct {
	Challenge      string `json:"challenge"`
	Subject        string `json:"subject"`
	Client         struct {
		ClientID string `json:"client_id"`
	} `json:"client"`
	RequestedScope  []string `json:"requested_scope"`
	RequestedAccessTokenAudience []string `json:"requested_access_token_audience"`
}

// AcceptConsentBody is the body sent to accept a consent request.
type AcceptConsentBody struct {
	GrantScope               []string               `json:"grant_scope"`
	GrantAccessTokenAudience []string               `json:"grant_access_token_audience,omitempty"`
	Remember                 bool                   `json:"remember"`
	RememberFor              int                    `json:"remember_for,omitempty"`
	Session                  *AcceptConsentSession  `json:"session,omitempty"`
}

// AcceptConsentSession carries session data for the ID token and access token.
type AcceptConsentSession struct {
	IDToken     map[string]any `json:"id_token,omitempty"`
	AccessToken map[string]any `json:"access_token,omitempty"`
}

// TokenResponse is returned from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// IntrospectionResponse is the OAuth2 introspection response.
type IntrospectionResponse struct {
	Active      bool                   `json:"active"`
	Scope       string                 `json:"scope"`
	ClientID    string                 `json:"client_id"`
	Subject     string                 `json:"sub"`
	TokenType   string                 `json:"token_type"`
	Exp         int64                  `json:"exp"`
	Iat         int64                  `json:"iat"`
	Nbf         int64                  `json:"nbf"`
	Aud         []string               `json:"aud"`
	Iss         string                 `json:"iss"`
	Extra       map[string]any `json:"ext"`
}

// ErrorResponse is returned on Hydra errors.
type ErrorResponse struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

// --- Admin API methods ---

// GetLoginRequest fetches the login challenge details from Hydra.
func (c *Client) GetLoginRequest(ctx context.Context, challenge string) (*LoginRequest, error) {
	url := fmt.Sprintf("%s/admin/oauth2/auth/requests/login?login_challenge=%s", c.adminURL, url.QueryEscape(challenge))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("get login request: %w", err)
	}

	var result LoginRequest
	if err := c.doJSON(req, &result); err != nil {
		return nil, fmt.Errorf("get login request: %w", err)
	}
	return &result, nil
}

// AcceptLogin accepts the login challenge and returns the redirect URL.
func (c *Client) AcceptLogin(ctx context.Context, challenge string, body *AcceptLoginBody) (string, error) {
	url := fmt.Sprintf("%s/admin/oauth2/auth/requests/login/accept?login_challenge=%s", c.adminURL, url.QueryEscape(challenge))
	return c.putLoginConsent(ctx, url, body)
}

// RejectLogin rejects the login challenge and returns the redirect URL.
func (c *Client) RejectLogin(ctx context.Context, challenge string, reason string) (string, error) {
	rejectURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/login/reject?login_challenge=%s", c.adminURL, url.QueryEscape(challenge))
	body := map[string]string{"error": "access_denied", "error_description": reason}
	return c.putLoginConsent(ctx, rejectURL, body)
}

// GetConsentRequest fetches the consent challenge details from Hydra.
func (c *Client) GetConsentRequest(ctx context.Context, challenge string) (*ConsentRequest, error) {
	url := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent?consent_challenge=%s", c.adminURL, url.QueryEscape(challenge))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("get consent request: %w", err)
	}

	var result ConsentRequest
	if err := c.doJSON(req, &result); err != nil {
		return nil, fmt.Errorf("get consent request: %w", err)
	}
	return &result, nil
}

// AcceptConsent accepts the consent challenge and returns the redirect URL.
func (c *Client) AcceptConsent(ctx context.Context, challenge string, body *AcceptConsentBody) (string, error) {
	url := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent/accept?consent_challenge=%s", c.adminURL, url.QueryEscape(challenge))
	return c.putLoginConsent(ctx, url, body)
}

// RejectConsent rejects the consent challenge and returns the redirect URL.
func (c *Client) RejectConsent(ctx context.Context, challenge string, reason string) (string, error) {
	rejectURL := fmt.Sprintf("%s/admin/oauth2/auth/requests/consent/reject?consent_challenge=%s", c.adminURL, url.QueryEscape(challenge))
	body := map[string]string{"error": "access_denied", "error_description": reason}
	return c.putLoginConsent(ctx, rejectURL, body)
}

// --- Public API methods ---

// ExchangeCode exchanges an authorization code for tokens (PKCE).
func (c *Client) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, clientID string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("code_verifier", codeVerifier)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", clientID)

	return c.requestToken(ctx, data)
}

// RefreshToken exchanges a refresh token for new tokens.
func (c *Client) RefreshToken(ctx context.Context, refreshToken, clientID string) (*TokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", clientID)

	return c.requestToken(ctx, data)
}

// IntrospectToken introspects an access token.
func (c *Client) IntrospectToken(ctx context.Context, token, clientID, clientSecret string) (*IntrospectionResponse, error) {
	introspectURL := fmt.Sprintf("%s/oauth2/introspect", c.publicURL)

	data := url.Values{}
	data.Set("token", token)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, introspectURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("introspect: %w", err)
	}
	req.SetBasicAuth(clientID, clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var result IntrospectionResponse
	if err := c.doJSON(req, &result); err != nil {
		return nil, fmt.Errorf("introspect: %w", err)
	}
	return &result, nil
}

// --- helpers ---

func (c *Client) putLoginConsent(ctx context.Context, url string, body any) (string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var result AcceptLoginResponse
	if err := c.doJSON(req, &result); err != nil {
		return "", fmt.Errorf("put: %w", err)
	}
	return result.RedirectTo, nil
}

func (c *Client) requestToken(ctx context.Context, data url.Values) (*TokenResponse, error) {
	tokenURL := fmt.Sprintf("%s/oauth2/token", c.publicURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var result TokenResponse
	if err := c.doJSON(req, &result); err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	return &result, nil
}

func (c *Client) doJSON(req *http.Request, v any) error {
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil {
			return fmt.Errorf("hydra error: %s — %s", errResp.Error, errResp.Description)
		}
		return fmt.Errorf("hydra HTTP %d: %s", resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}
