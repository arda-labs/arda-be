package provider

import (
	"context"
	"time"
)

// Type classifies an authentication provider.
type Type string

const (
	TypePassword Type = "password"
	TypeOIDC     Type = "oidc"
	TypeSAML     Type = "saml"
	TypeLDAP     Type = "ldap"
)

// Metadata describes a provider for the login UI.
type Metadata struct {
	ID          string `json:"id"`
	Type        Type   `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	ButtonColor string `json:"button_color"`
	Priority    int    `json:"priority"`
	IsPrimary   bool   `json:"is_primary"`
	IsEnabled   bool   `json:"is_enabled"`
	Domains     []string `json:"domains,omitempty"`
}

// AuthenticationResult is the unified result from any provider.
type AuthenticationResult struct {
	ExternalID   string     `json:"external_id"`
	ProviderID   string     `json:"provider_id"`
	ProviderType Type       `json:"provider_type"`

	Claims       map[string]any `json:"claims,omitempty"`

	InternalUserID string `json:"internal_user_id"`

	ACR      string   `json:"acr"`
	AMR      []string `json:"amr"`
	AuthTime time.Time `json:"auth_time"`

	SessionData *SessionData `json:"session_data,omitempty"`
}

// SessionData carries session attributes for Hydra.
type SessionData struct {
	Subject           string         `json:"subject"`
	IDTokenExtra      map[string]any `json:"id_token_extra,omitempty"`
	AccessTokenExtra  map[string]any `json:"access_token_extra,omitempty"`
}

// InitiateRequest starts an interactive (redirect-based) authentication.
type InitiateRequest struct {
	LoginChallenge string
	RedirectURI    string
	State          string
	Hints          map[string]string
}

// InitiateResponse returns the redirect URL for interactive auth.
type InitiateResponse struct {
	RedirectURL string            `json:"redirect_url"`
	Data        map[string]string `json:"data,omitempty"`
}

// CallbackRequest carries the callback parameters from an external IdP.
type CallbackRequest struct {
	State        string
	Code         string
	SAMLResponse string
	QueryParams  map[string]string
	FormParams   map[string]string
}

// DirectAuthRequest carries credentials for direct authentication (password, LDAP).
type DirectAuthRequest struct {
	LoginChallenge string
	Credential     map[string]string
}

// AuthenticationProvider is the interface that all auth providers implement.
type AuthenticationProvider interface {
	// Metadata returns display and configuration info.
	Metadata() Metadata

	// Validate checks the provider is configured correctly at startup.
	Validate(ctx context.Context) error

	// SupportsInteractive returns true for redirect-based providers (OIDC, SAML).
	SupportsInteractive() bool

	// InitiateAuthentication starts an interactive auth flow.
	InitiateAuthentication(ctx context.Context, req *InitiateRequest) (*InitiateResponse, error)

	// HandleCallback processes the callback from an external IdP.
	HandleCallback(ctx context.Context, req *CallbackRequest) (*AuthenticationResult, error)

	// SupportsDirect returns true for credential-based providers (password, LDAP).
	SupportsDirect() bool

	// AuthenticateDirect authenticates with direct credentials.
	AuthenticateDirect(ctx context.Context, req *DirectAuthRequest) (*AuthenticationResult, error)
}
